// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
)

type origin struct {
	name  string
	cache *cache
}

func newOrigin(name string, f *fetcher, cf *config) *origin {
	return &origin{
		name:  name,
		cache: newCache(f, cf),
	}
}

func (o *origin) handle(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	cg := group(vars["q"])
	if cg == "" {
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
	page := o.cache.get(cg, n)
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
