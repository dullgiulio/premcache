// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

const intergatorTmpl = "https://search.kuehne-nagel.com/web/ig-kn/?q=%s&of=%d"

type resource struct {
	str string
	cg  group
	n   int
}

func newResource(tmpl string, cg group, n int) *resource {
	return &resource{
		cg:  cg,
		n:   n,
		str: fmt.Sprintf(tmpl, url.QueryEscape(string(cg)), n*10), // TODO: offset should not be hardcoded
	}
}

func (r *resource) String() string {
	return r.str
}

func (r *resource) cache(c *cache, body []byte, err error) {
	debug("url adds response to cache")
	c.put(r.cg, newPage(r.n, body), err)
}

type job struct {
	res   *resource
	cache *cache
}

func newJob(r *resource, c *cache) *job {
	return &job{res: r, cache: c}
}

func (j *job) get() ([]byte, error) {
	tr := &http.Transport{
		MaxIdleConns:    10,               // TODO: not hardcoded
		IdleConnTimeout: 30 * time.Second, // TODO: not hardcoded
	}
	client := &http.Client{Transport: tr}
	resp, err := client.Get(j.res.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (j *job) run() {
	debug("fetch request for %s", j.res)
	body, err := j.get()
	if err != nil {
		err = fmt.Errorf("cannot fetch URL %s: %s", j.res, err)
	}
	j.res.cache(j.cache, body, err)
}

type fetcher struct {
	jobs chan *job
}

func newFetcher(workers, queue int) *fetcher {
	f := &fetcher{
		jobs: make(chan *job, queue),
	}
	for i := 0; i < workers; i++ {
		go f.run()
	}
	return f
}

func (f *fetcher) run() {
	for job := range f.jobs {
		job.run()
	}
}

func (f *fetcher) request(j *job) {
	f.jobs <- j
}

type page struct {
	n      int
	body   []byte
	expire time.Time
	cached bool
}

func newPage(n int, body []byte) *page {
	return &page{
		n:    n,
		body: body,
	}
}

func (p *page) WriteTo(w io.Writer) (int, error) {
	n, err := w.Write(p.body)
	return n, err
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

func (ce *entry) asPage(n int) *page {
	p := newPage(n, ce.data)
	p.expire = ce.deadline
	return p
}

type waiters struct {
	waits map[group]map[int]chan struct{}
}

func newWaiters() *waiters {
	return &waiters{
		waits: make(map[group]map[int]chan struct{}),
	}
}

func (w *waiters) wait(cg group, n int) chan struct{} {
	if _, ok := w.waits[cg]; !ok {
		w.waits[cg] = make(map[int]chan struct{})
	} else {
		if ch, ok := w.waits[cg][n]; ok {
			return ch
		}
	}
	ch := make(chan struct{})
	w.waits[cg][n] = ch
	return ch
}

func (w *waiters) has(cg group, n int) bool {
	_, ok := w.waits[cg]
	if !ok {
		return false
	}
	_, ok = w.waits[cg][n]
	return ok
}

func (w *waiters) done(cg group, n int) {
	_, ok := w.waits[cg]
	if !ok {
		return
	}
	ch, ok := w.waits[cg][n]
	if !ok {
		return
	}
	close(ch)
	delete(w.waits[cg], n)
	// Cleanup
	if len(w.waits[cg]) == 0 {
		delete(w.waits, cg)
	}
}

type entries struct {
	ents map[group]map[int]*entry
}

func newEntries() *entries {
	return &entries{
		ents: make(map[group]map[int]*entry),
	}
}

func (e *entries) get(cg group, n int) (*entry, bool) {
	pages, ok := e.ents[cg]
	if !ok {
		return nil, false
	}
	ce, ok := pages[n]
	return ce, ok
}

func (e *entries) put(cg group, n int, ce *entry) {
	_, ok := e.ents[cg]
	if !ok {
		e.ents[cg] = make(map[int]*entry)
	}
	e.ents[cg][n] = ce
}

func (e *entries) has(cg group, n int) bool {
	_, ok := e.ents[cg]
	if !ok {
		return false
	}
	_, ok = e.ents[cg][n]
	return ok
}

// remove assumes the entry exists
func (e *entries) remove(cg group, n int) {
	delete(e.ents[cg], n)
	if len(e.ents[cg]) == 0 {
		delete(e.ents, cg)
	}
}

func (e *entries) gc(t time.Time) {
	for cg, ents := range e.ents {
		for n := range ents {
			if ents[n].invalid(t) {
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
	events  chan cacheFunc
}

func newCache(f *fetcher) *cache {
	c := &cache{
		events:  make(chan cacheFunc),
		fetcher: f,
		entries: newEntries(),
		waits:   newWaiters(),
	}
	go c.run()
	go c.gc(5 * time.Second) // TODO: not hardcoded
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
			c.entries.gc(time.Now())
			done <- struct{}{}
			return nil
		}
		<-done
	}
}

// put inserts a page into the cache (after it was fetched).
func (c *cache) put(cg group, p *page, err error) {
	c.events <- func() error {
		ce := newEntry(p.body, 2*time.Minute) // TODO: not hardcoded
		c.entries.put(cg, p.n, ce)
		debug("added page %s/%d", cg, p.n)
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
	for i := 0; i < n+m; i++ {
		if i == n {
			// just fetched
			continue
		}
		if c.entries.has(cg, i) || c.waits.has(cg, i) {
			// already fetched or requested
			continue
		}
		c.waits.wait(cg, i)
		res := newResource(intergatorTmpl, cg, i)
		c.fetcher.request(newJob(res, c))
	}
}

func (c *cache) request(cg group, n int) chan struct{} {
	wait := c.waits.wait(cg, n)
	res := newResource(intergatorTmpl, cg, n)
	c.fetcher.request(newJob(res, c))
	c.prefetch(cg, n, 3) // TODO: Number of prefetched pages not hardcoded
	return wait
}

func (c *cache) get(cg group, n int) *page {
	var (
		page *page
		wait chan struct{}
	)
	cached := true
	requested := make(chan struct{})
	for {
		wait = nil
		c.events <- func() error {
			defer func() { requested <- struct{}{} }()
			var now time.Time
			ce, ok := c.entries.get(cg, n)
			if ok {
				now = time.Now()
			}
			if !ok || ce.invalid(now) {
				debug("%s/%d: not cached, requested", cg, n)
				wait = c.request(cg, n)
				return nil
			} else {
				debug("%s/%d: found", cg, n)
				c.prefetch(cg, n, 3) // TODO: Number of prefetched pages not hardcoded
			}
			page = ce.asPage(n)
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
		debug("%s/%d: waiting", cg, n)
		// content is being fetched, wait and try to get again
		<-wait
	}
	// unreacheable
	return nil
}

type origin struct {
	name  string
	cache *cache
}

func newOrigin(name string, f *fetcher) *origin {
	return &origin{
		name:  name,
		cache: newCache(f),
	}
}

func (o *origin) handle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	q := vars["q"]
	if q == "" {
		http.NotFound(w, r)
		return
	}
	n := 0
	if vars["n"] != "" {
		m, err := strconv.ParseInt(vars["n"], 10, 64)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		n = int(m)
	}
	debug("%s/%d: requesting from cache", q, n)
	page := o.cache.get(group(q), n)
	if page.cached {
		w.Header().Set("X-From-Cache", "1")
	}
	w.Header().Set("X-Cached-Until", page.expire.Format(time.RFC3339))
	if _, err := page.WriteTo(w); err != nil {
		log.Printf("http: error writing response body: %s", err)
	}
}

type origins struct {
	o map[string]*origin
}

func newOrigins() *origins {
	return &origins{
		o: make(map[string]*origin),
	}
}

func (ors *origins) add(o *origin) {
	ors.o[o.name] = o
}

func (ors *origins) initRouter(r *mux.Router) {
	for k := range ors.o {
		r.HandleFunc(fmt.Sprintf("/%s/search/{q}", ors.o[k].name), ors.o[k].handle)
		r.HandleFunc(fmt.Sprintf("/%s/search/{q}/{n}", ors.o[k].name), ors.o[k].handle)
	}
}

func main() {
	fetcher := newFetcher(10, 20) // TODO: From arguments
	origins := newOrigins()
	origins.add(newOrigin("intergator", fetcher))

	r := mux.NewRouter()
	origins.initRouter(r)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(":8383", nil))
}
