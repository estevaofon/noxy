//go:build unix

package main

import (
	"os/signal"
	"syscall"
)

func prepareOutputSignals() {
	signal.Ignore(syscall.SIGPIPE)
}
