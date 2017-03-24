// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "time"

type config struct {
	lifetime time.Duration
	gcpause  time.Duration
	tmpl     string
	npref    int
	incr     int
}

func newConfig(tmpl string, incr int) *config {
	return &config{
		tmpl:     tmpl,
		incr:     incr,
		lifetime: 5 * time.Minute,
		gcpause:  20 * time.Second,
		npref:    4,
	}
}
