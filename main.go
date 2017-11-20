// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/mux"
)

const intergatorTmpl = "https://search.kuehne-nagel.com/web/ig-kn/?q=%s&of=%d"

func main() {
	var (
		verbose        bool
		name           string
		tmpl           string
		listen         string
		nlogs          int
		incr           int
		maxmem         int
		gcpause        int
		gclifetime     int
		fetcherPages   int
		fetcherQueue   int
		fetcherWorkers int
	)
	// TODO: Could support multiple caches if it was possible to pass them as args.
	flag.BoolVar(&verbose, "verbose", false, "Print detailed debug messages")
	flag.StringVar(&listen, "listen", "0.0.0.0:8383", "Address and port to listen to")
	flag.StringVar(&name, "name", "intergator", "Cache name; will be shown in URL")
	flag.StringVar(&tmpl, "tmpl", intergatorTmpl, "URL template") // TODO: For real
	flag.IntVar(&incr, "incr", 10, "Increment of offset counter for each page")
	flag.IntVar(&fetcherWorkers, "fworkers", 10, "Fetcher workers parallel routines")
	flag.IntVar(&fetcherQueue, "fqueue", 20, "Fetcher workers queue size")
	flag.IntVar(&maxmem, "mem", 256, "Max memory to use for cached entries, in MB") // TODO: Parse size
	flag.IntVar(&fetcherPages, "npref", 4, "Number of pages to prefetch")
	flag.IntVar(&gclifetime, "lifetime", 5, "Time an entry is kept in cache, in minutes")     // TODO: Parse time
	flag.IntVar(&gcpause, "gcpause", 20, "Interval between GC runs in the cache, in seconds") // TODO: Parse time
	flag.IntVar(&nlogs, "nlogs", 100, "Max number of lines of logs to store for each origin")
	flag.Parse()

	config := newConfig(tmpl, incr)
	config.npref = fetcherPages
	config.maxMemory = 1024 * 1024 * int64(maxmem)
	config.lifetime = time.Duration(gclifetime) * time.Minute
	config.gcpause = time.Duration(gcpause) * time.Second

	fetcher := newFetcher(fetcherWorkers, fetcherQueue)
	origins := newOrigins()
	origins.add(newOrigin(name, fetcher, config, newLogbuf(nlogs, verbose)))

	r := mux.NewRouter()
	origins.initRouter(r)

	http.Handle("/", r)
	log.Fatal(http.ListenAndServe(listen, nil))
}
