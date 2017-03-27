// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "time"

type config struct {
	lifetime  time.Duration
	gcpause   time.Duration
	tmpl      string
	npref     int
	incr      int
	maxMemory int64
}

func newConfig(tmpl string, incr int) *config {
	return &config{
		tmpl:      tmpl,
		incr:      incr,
		lifetime:  5 * time.Minute,
		gcpause:   20 * time.Second,
		npref:     4,
		maxMemory: 1024 * 1024 * 256, // 256MB
	}
}

type stats struct {
	Entries  int
	Waiters  int
	Requests int
	Cached   int
	Mem      int64
}

func newStats() *stats {
	return &stats{}
}

func (s *stats) mem(n int) {
	s.Mem += int64(n)
}

func (s *stats) hit(cached bool) {
	if cached {
		s.Cached++
	}
	s.Requests++
}

func (s *stats) above(mem int64) bool {
	return s.Mem >= mem
}

func (s *stats) clone() *stats {
	st := *s
	return &st
}
