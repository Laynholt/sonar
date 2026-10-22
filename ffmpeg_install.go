package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sonar/internal/convert"
	"sonar/internal/ffinstall"
	"sonar/internal/httpx"
)

// FFmpegInstall is the state of the FFmpeg download the Settings dialog polls.
type FFmpegInstall struct {
	Step     string `json:"step"` // "idle" | "download" | "install" | "done" | "error"
	Done     int64  `json:"done"`
	Total    int64  `json:"total"`
	Path     string `json:"path"` // the installed ffmpeg.exe when Step == "done"
	Error    string `json:"error"`
	NotEmpty bool   `json:"not_empty"` // the error was ffinstall.ErrNotEmpty
}

// writable is a variable so tests can fake it: it tries to create and remove a file in dir.
var writable = func(dir string) bool {
	f, err := os.CreateTemp(dir, ".sonar-write-*")
	if err != nil {
		return false
	}
	f.Close()
	return os.Remove(f.Name()) == nil
}

// DefaultFFmpegDir is <exe dir>\ffmpeg when that folder can be written, else %LOCALAPPDATA%\Sonar\ffmpeg with fallback=true.
func (a *App) DefaultFFmpegDir() (dir string, fallback bool) {
	if exe := convert.ExeDir(); exe != "" && writable(exe) {
		return filepath.Join(exe, "ffmpeg"), false
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Sonar", "ffmpeg"), true
}

// SelectFFmpegFolder returns the picked folder plus \ffmpeg, "" when the user cancels.
func (a *App) SelectFFmpegFolder() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{})
	if err != nil || dir == "" {
		return "", err
	}
	return filepath.Join(dir, "ffmpeg"), nil
}

// StartFFmpegInstall downloads and installs FFmpeg into dir in the background; poll GetFFmpegInstall.
func (a *App) StartFFmpegInstall(dir string) error {
	a.ffMu.Lock()
	defer a.ffMu.Unlock()
	if a.ffCancel != nil {
		return errors.New("an FFmpeg install is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.ffCancel = cancel
	a.ffState = FFmpegInstall{Step: "download"}
	go a.runFFmpegInstall(ctx, dir)
	return nil
}

func (a *App) runFFmpegInstall(ctx context.Context, dir string) {
	path, err := a.installFFmpeg(ctx, dir)
	cancelled := err != nil && ctx.Err() != nil // before ffCancel, which cancels ctx; a finished install stays done
	a.ffMu.Lock()
	defer a.ffMu.Unlock()
	a.ffCancel()
	a.ffCancel = nil
	switch {
	case cancelled:
		a.ffState = FFmpegInstall{Step: "error", Error: "cancelled"}
	case err != nil:
		a.ffState = FFmpegInstall{Step: "error", Error: err.Error(), NotEmpty: errors.Is(err, ffinstall.ErrNotEmpty)}
	default:
		a.ffState = FFmpegInstall{Step: "done", Path: path}
	}
}

func (a *App) installFFmpeg(ctx context.Context, dir string) (string, error) {
	archive, sums, err := a.ffLatest(ctx, httpx.Client(), ffinstall.ReleaseAPI)
	if err != nil {
		return "", err
	}
	return a.ffInstall(ctx, httpx.StreamClient(), archive, sums, dir, func(p ffinstall.Progress) {
		a.ffMu.Lock()
		a.ffState = FFmpegInstall{Step: p.Step, Done: p.Done, Total: p.Total}
		a.ffMu.Unlock()
	})
}

func (a *App) GetFFmpegInstall() FFmpegInstall {
	a.ffMu.Lock()
	defer a.ffMu.Unlock()
	if a.ffState.Step == "" {
		return FFmpegInstall{Step: "idle"}
	}
	return a.ffState
}

func (a *App) CancelFFmpegInstall() {
	a.ffMu.Lock()
	defer a.ffMu.Unlock()
	if a.ffCancel != nil {
		a.ffCancel()
	}
}
