//go:build !windows

package main

import (
	"os"
	"syscall"
)

// terminationSignals returns OS signals that should trigger graceful shutdown.
func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
