// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"io"
	"log"
	"sort"
	"time"
)

type page struct {
	n      offset
	body   []byte
	expire time.Time
	cached bool
}

func newPage(n offset, body []byte) *page {
	return &page{
		n:    n,
		body: body,
	}
}

func (p *page) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(p.body)
	return int64(n), err
}

type group string

type entry struct {
	deadline time.Time
	data     []byte
}

func newEntry(data []byte, d time.Duration) *entry {
	return &entry{
		deadline: time.Now().Add(d),
		data:     data,
	}
}

func (ce *entry) invalid(t time.Time) bool {
	return !ce.deadline.After(t)
}

func (ce *entry) asPage(n offset) *page {
	p := newPage(n, ce.data)
	p.expire = ce.deadline
	return p
}

type entries struct {
	ents map[group]map[offset]*entry
}

func newEntries() *entries {
	return &entries{
		ents: make(map[group]map[offset]*entry),
	}
}

func (e *entries) count() int {
	tot := 0
	for k := range e.ents {
		tot += len(e.ents[k])
	}
	return tot
}

func (e *entries) get(cg group, n offset) (*entry, bool) {
	pages, ok := e.ents[cg]
	if !ok {
		return nil, false
	}
	ce, ok := pages[n]
	return ce, ok
}

func (e *entries) put(cg group, n offset, ce *entry) {
	_, ok := e.ents[cg]
	if !ok {
		e.ents[cg] = make(map[offset]*entry)
	}
	e.ents[cg][n] = ce
}

func (e *entries) has(cg group, n offset) bool {
	_, ok := e.ents[cg]
	if !ok {
		return false
	}
	_, ok = e.ents[cg][n]
	return ok
}

// remove assumes the entry exists
func (e *entries) remove(cg group, n offset) {
	delete(e.ents[cg], n)
	if len(e.ents[cg]) == 0 {
		delete(e.ents, cg)
	}
}

func (e *entries) purge(cg group, st *stats) {
	st.mem(-e.sizeof(cg))
	delete(e.ents, cg)
}

func (e *entries) sizeof(cg group) int {
	var s int
	for n := range e.ents[cg] {
		s += len(e.ents[cg][n].data)
	}
	return s
}

func (e *entries) oldestDeadline(cg group) time.Time {
	var t time.Time
	for off := range e.ents[cg] {
		entry := e.ents[cg][off]
		if t.IsZero() || t.After(entry.deadline) {
			t = entry.deadline
		}
	}
	return t
}

func (e *entries) gc(t time.Time, st *stats) {
	for cg, ents := range e.ents {
		for n := range ents {
			if ents[n].invalid(t) {
				st.mem(-len(ents[n].data))
				e.remove(cg, n)
			}
		}
	}
}

type cacheFunc func() error

type cache struct {
	entries *entries
	waits   *waiters
	fetcher *fetcher
	config  *config
	stat    *stats
	events  chan cacheFunc
}

func newCache(f *fetcher, cf *config) *cache {
	c := &cache{
		fetcher: f,
		config:  cf,
		events:  make(chan cacheFunc),
		entries: newEntries(),
		waits:   newWaiters(),
		stat:    newStats(),
	}
	go c.gc(cf.gcpause)
	go c.run()
	return c
}

func (c *cache) run() {
	for f := range c.events {
		if err := f(); err != nil {
			log.Print("cache: ", err)
		}
	}
}

func (c *cache) gc(d time.Duration) {
	done := make(chan struct{})
	for {
		time.Sleep(d)
		c.events <- func() error {
			debug("running garbage collector cycle, memory is %d", c.stat.Mem)
			c.entries.gc(time.Now(), c.stat)
			debug("garbage collection done, memory is %d", c.stat.Mem)
			done <- struct{}{}
			return nil
		}
		<-done
	}
}

type timeGroup struct {
	t  time.Time
	cg group
}

type timeGroups struct {
	entries []timeGroup
}

type byTime []timeGroup

func (a byTime) Len() int           { return len(a) }
func (a byTime) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byTime) Less(i, j int) bool { return a[i].t.After(a[j].t) }

func makeTimeGroups(e *entries) timeGroups {
	var i int
	tg := make([]timeGroup, len(e.ents))
	for cg := range e.ents {
		tg[i] = timeGroup{
			t:  e.oldestDeadline(cg),
			cg: cg,
		}
		i++
	}
	sort.Sort(byTime(tg))
	return timeGroups{tg}
}

func (tg *timeGroups) purgeOldest(c *cache) {
	entry, ts := tg.entries[len(tg.entries)-1], tg.entries[0:len(tg.entries)-1]
	c.entries.purge(entry.cg, c.stat)
	tg.entries = ts
}

func (tg *timeGroups) empty() bool {
	return len(tg.entries) == 0
}

func (c *cache) oom(target int64) {
	debug("OOM called: using %d, limit is %d", c.stat.Mem, target)
	tg := makeTimeGroups(c.entries)
	for {
		tg.purgeOldest(c)
		// Repeat unless we have no more entries or we are using less memory than target
		if tg.empty() || !c.stat.above(target) {
			break
		}
	}
	debug("OOM: mem now %d", c.stat.Mem)
}

// put inserts a page into the cache (after it was fetched).
func (c *cache) put(cg group, p *page, err error) {
	c.events <- func() error {
		ce := newEntry(p.body, c.config.lifetime)
		c.entries.put(cg, p.n, ce)
		debug("added page %s/%d", cg, p.n)
		c.stat.mem(len(p.body))
		if c.stat.above(c.config.maxMemory) {
			go func() {
				c.events <- func() error {
					c.oom(c.config.maxMemory)
					return nil
				}
			}()
		}
		// If there were waiters, signal that the wait is over
		c.waits.done(cg, p.n)
		return err
	}
}

// prefetch requests pages (n-m, n) and (n, n+m) if not already fetched
func (c *cache) prefetch(cg group, n, m int) {
	i := n - m
	if i < 0 {
		i = 0
	}
	for ; i < n+m; i++ {
		if i == n {
			// just fetched
			continue
		}
		off := offset(i * c.config.incr)
		if c.entries.has(cg, off) || c.waits.has(cg, off) {
			// already fetched or requested
			continue
		}
		c.waits.wait(cg, off)
		res := newResource(c.config.tmpl, cg, off)
		c.fetcher.request(newJob(res, c))
	}
}

func (c *cache) request(cg group, n int) chan struct{} {
	off := offset(n * c.config.incr)
	wait := c.waits.wait(cg, off)
	res := newResource(c.config.tmpl, cg, off)
	c.fetcher.request(newJob(res, c))
	c.prefetch(cg, n, c.config.npref)
	return wait
}

func (c *cache) stats() *stats {
	var st *stats
	wait := make(chan struct{})
	c.events <- func() error {
		c.stat.Entries = c.entries.count()
		c.stat.Waiters = c.waits.count()
		st = c.stat.clone()
		wait <- struct{}{}
		return nil
	}
	<-wait
	return st
}

func (c *cache) get(cg group, n int) *page {
	var (
		page *page
		wait chan struct{}
	)
	cached := true
	requested := make(chan struct{})
	off := offset(n * c.config.incr)
	debug("%s/%d: requesting from cache", cg, off)
	for {
		wait = nil
		c.events <- func() error {
			defer func() { requested <- struct{}{} }()
			var now time.Time
			ce, ok := c.entries.get(cg, off)
			if ok {
				now = time.Now()
			}
			if !ok || ce.invalid(now) {
				debug("%s/%d: not cached, requested", cg, off)
				wait = c.request(cg, n)
				return nil
			}
			debug("%s/%d: found", cg, off)
			c.prefetch(cg, n, c.config.npref)
			c.stat.hit(cached)
			page = ce.asPage(off)
			return nil
		}
		<-requested
		// content was already in cache, return it
		if wait == nil {
			page.cached = cached
			return page
		}
		// We needed to request the object, it was not cached
		cached = false
		// content is being fetched, wait and try to get again
		<-wait
	}
}
