//go:build windows

package browser

import "syscall"

// launchSysProcAttr hides the console window a browser spawn would flash.
func launchSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
