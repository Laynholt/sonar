//go:build !windows

package ffinstall

import "os/exec"

func hideWindow(*exec.Cmd) {}
