//go:build windows

package plugin

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a plugin's console from flashing (CREATE_NO_WINDOW).
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
