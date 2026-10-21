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
	switch runtime.GOOS {
	case "windows":
		// explorer exits 1 even when it opened the folder, so its status is ignored.
		_ = exec.Command("explorer", path).Start()
		return nil
	case "darwin":
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}
