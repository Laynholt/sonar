//go:build windows

package doubledouble

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// askToClose sends the browser's windows a close message (taskkill without /F), with no console flash.
func askToClose(p *os.Process) {
	cmd := exec.Command("taskkill", "/PID", strconv.Itoa(p.Pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	cmd.Run()
}
