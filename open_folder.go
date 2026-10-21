package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// OpenFolder shows path in the file manager: a directory itself, a file's directory otherwise.
func (a *App) OpenFolder(path string) error {
	if st, err := os.Stat(path); err != nil {
		return err
	} else if !st.IsDir() {
		path = filepath.Dir(path)
	}
	name := "xdg-open"
	switch runtime.GOOS {
	case "windows":
		name = "explorer"
	case "darwin":
		name = "open"
	}
	cmd := exec.Command(name, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reaped in the background; explorer exits 1 even on success, so the status is ignored.
	go cmd.Wait()
	return nil
}
