package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sonar/internal/download"
	"sonar/internal/ffinstall"
)

func fakeLatest(context.Context, *http.Client, string) (ffinstall.Asset, ffinstall.Asset, error) {
	return ffinstall.Asset{Name: "a.zip"}, ffinstall.Asset{Name: "sums"}, nil
}

func waitFF(t *testing.T, a *App, step string) FFmpegInstall {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if s := a.GetFFmpegInstall(); s.Step == step {
			return s
		}
	}
	t.Fatalf("state never reached %q: %+v", step, a.GetFFmpegInstall())
	return FFmpegInstall{}
}

func TestStartFFmpegInstallReportsSteps(t *testing.T) {
	release, reported := make(chan struct{}), make(chan struct{})
	dir := filepath.Join(t.TempDir(), "ff")
	a := &App{ffLatest: fakeLatest}
	a.ffInstall = func(_ context.Context, _ *http.Client, _, _ ffinstall.Asset, dir string, report func(ffinstall.Progress)) (string, error) {
		report(ffinstall.Progress{Step: "download", Done: 5, Total: 10})
		close(reported)
		<-release
		return filepath.Join(dir, "ffmpeg.exe"), nil
	}
	if err := a.StartFFmpegInstall(dir); err != nil {
		t.Fatal(err)
	}
	<-reported
	if s := a.GetFFmpegInstall(); s.Step != "download" || s.Done != 5 || s.Total != 10 {
		t.Errorf("state = %+v", s)
	}
	if err := a.StartFFmpegInstall(dir); err == nil {
		t.Error("second Start while running must fail")
	}
	close(release)
	if s := waitFF(t, a, "done"); s.Path != filepath.Join(dir, "ffmpeg.exe") || s.Error != "" {
		t.Errorf("state = %+v", s)
	}
}

func TestFFmpegInstallNotEmpty(t *testing.T) {
	a := &App{ffLatest: fakeLatest}
	a.ffInstall = func(context.Context, *http.Client, ffinstall.Asset, ffinstall.Asset, string, func(ffinstall.Progress)) (string, error) {
		return "", ffinstall.ErrNotEmpty
	}
	if err := a.StartFFmpegInstall(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if s := waitFF(t, a, "error"); !s.NotEmpty || s.Error == "" {
		t.Errorf("state = %+v", s)
	}
}

func TestCancelFFmpegInstall(t *testing.T) {
	started := make(chan struct{})
	a := &App{ffLatest: fakeLatest}
	a.ffInstall = func(ctx context.Context, _ *http.Client, _, _ ffinstall.Asset, _ string, _ func(ffinstall.Progress)) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}
	if err := a.StartFFmpegInstall(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	<-started
	a.CancelFFmpegInstall()
	if s := waitFF(t, a, "error"); s.Error != "cancelled" || s.NotEmpty {
		t.Errorf("state = %+v", s)
	}
	// a finished run can be started again
	a.ffInstall = func(context.Context, *http.Client, ffinstall.Asset, ffinstall.Asset, string, func(ffinstall.Progress)) (string, error) {
		return "p", errors.New("boom")
	}
	if err := a.StartFFmpegInstall(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if s := waitFF(t, a, "error"); s.Error != "boom" {
		t.Errorf("state = %+v", s)
	}
}

func TestStartFFmpegInstallRejectsRelativeDir(t *testing.T) {
	a := &App{ffLatest: fakeLatest}
	if err := a.StartFFmpegInstall("ffmpeg"); err == nil {
		t.Error("relative dir must be rejected")
	}
	if s := a.GetFFmpegInstall(); s.Step != "idle" || a.ffCancel != nil {
		t.Errorf("a rejected start left state %+v", s)
	}
}

func TestDownloadTracksNoFFmpegSentinel(t *testing.T) {
	a := &App{findFFmpeg: func(string) string { return "" }}
	err := a.DownloadTracks(DownloadRequest{Options: download.Options{Format: "mp3-320"}})
	has := err != nil && strings.HasPrefix(err.Error(), "NO_FFMPEG: ")
	if err == nil || has != (runtime.GOOS == "windows") {
		t.Errorf("err = %v on %s", err, runtime.GOOS)
	}
}

func TestDefaultFFmpegDirFallback(t *testing.T) {
	old := writable
	defer func() { writable = old }()
	local := t.TempDir()
	t.Setenv("LOCALAPPDATA", local)

	writable = func(string) bool { return false }
	got := (&App{}).DefaultFFmpegDir()
	if want := filepath.Join(local, "Sonar", "ffmpeg"); got.Dir != want || !got.Fallback {
		t.Errorf("unwritable: %+v", got)
	}
	writable = func(string) bool { return true }
	got = (&App{}).DefaultFFmpegDir()
	if filepath.Base(got.Dir) != "ffmpeg" || got.Fallback || strings.HasPrefix(got.Dir, local) {
		t.Errorf("writable: %+v", got)
	}
}

func TestWritable(t *testing.T) {
	d := t.TempDir()
	if !writable(d) {
		t.Error("temp dir must be writable")
	}
	if es, _ := os.ReadDir(d); len(es) != 0 {
		t.Errorf("probe left files: %v", es)
	}
	if writable(filepath.Join(d, "missing")) {
		t.Error("missing dir must not be writable")
	}
}
