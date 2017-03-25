// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type offset int

type resource struct {
	str string
	cg  group
	n   offset
}

func newResource(tmpl string, cg group, n offset) *resource {
	return &resource{
		cg:  cg,
		n:   n,
		str: fmt.Sprintf(tmpl, url.QueryEscape(string(cg)), n),
	}
}

func (r *resource) String() string {
	return r.str
}

func (r *resource) cache(c *cache, body []byte, err error) {
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
		return nil, fmt.Errorf("cannot GET %s: %s", j.res, err)
	}
	defer resp.Body.Close()
	buf := &bytes.Buffer{}
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot copy data from %s: %s", j.res, err)
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
