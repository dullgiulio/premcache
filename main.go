// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

const intergatorTmpl = "https://search.kuehne-nagel.com/web/ig-kn/?q=%s&of=%d"

func main() {
	config := newConfig(intergatorTmpl, 10) // TODO: not hardcoded
	fetcher := newFetcher(10, 20)           // TODO: From arguments
	origins := newOrigins()
	origins.add(newOrigin("intergator", fetcher, config))

	r := mux.NewRouter()
	origins.initRouter(r)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(":8383", nil))
}
