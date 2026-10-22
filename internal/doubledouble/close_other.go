//go:build !windows

package doubledouble

import (
	"os"
	"syscall"
)

// askToClose sends SIGTERM, which Chromium handles as a normal quit.
func askToClose(p *os.Process) { p.Signal(syscall.SIGTERM) }
