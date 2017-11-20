package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
)

type logbuf struct {
	mux     sync.Mutex
	verbose bool
	lines   []string
	pos     int
}

func newLogbuf(n int, verb bool) *logbuf {
	return &logbuf{
		lines:   make([]string, n),
		verbose: verb,
	}
}

func (l *logbuf) debug(format string, args ...interface{}) {
	line := fmt.Sprintf(format, args...)
	if l.verbose {
		log.Print("debug: " + line)
	}
	l.mux.Lock()
	l.lines[l.pos] = fmt.Sprintf("%s: %s\n", time.Now().Format(time.RFC3339), line)
	l.pos = (l.pos + 1) % len(l.lines)
	l.mux.Unlock()
}

func (l *logbuf) WriteTo(w io.Writer) (int64, error) {
	var buf bytes.Buffer
	l.mux.Lock()
	pos := l.pos
	for {
		if l.lines[pos] != "" {
			buf.WriteString(l.lines[pos])
		}
		pos = (pos + 1) % len(l.lines)
		if pos == l.pos {
			break
		}
	}
	l.mux.Unlock()
	return buf.WriteTo(w)
}
