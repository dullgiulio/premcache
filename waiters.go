// Copyright 2017 Giulio Iotti. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type waiters struct {
	waits map[group]map[offset]chan struct{}
}

func newWaiters() *waiters {
	return &waiters{
		waits: make(map[group]map[offset]chan struct{}),
	}
}

func (w *waiters) count() int {
	tot := 0
	for k := range w.waits {
		tot += len(w.waits[k])
	}
	return tot
}

func (w *waiters) wait(cg group, n offset) chan struct{} {
	if _, ok := w.waits[cg]; !ok {
		w.waits[cg] = make(map[offset]chan struct{})
	} else {
		if ch, ok := w.waits[cg][n]; ok {
			return ch
		}
	}
	ch := make(chan struct{})
	w.waits[cg][n] = ch
	return ch
}

func (w *waiters) has(cg group, n offset) bool {
	_, ok := w.waits[cg]
	if !ok {
		return false
	}
	_, ok = w.waits[cg][n]
	return ok
}

func (w *waiters) done(cg group, n offset) {
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
