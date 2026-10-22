//go:build windows

package ffinstall

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps ffmpeg's console from flashing (CREATE_NO_WINDOW).
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
