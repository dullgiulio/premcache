package main

import "log"

var verbose bool

func debug(fmt string, args ...interface{}) {
	if verbose {
		log.Printf("debug: "+fmt, args...)
	}
}
