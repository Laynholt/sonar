package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sonar/internal/doubledouble"
	"sonar/internal/download"
	"sonar/internal/source"
)

type fakeSource string

func (f fakeSource) Name() string { return string(f) }
func (f fakeSource) Download(context.Context, string, string, func(string)) (string, error) {
	return "", nil
}

type fetched struct {
	o    download.Options
	path string
}

// newTestApp builds an App whose queue records what it was asked to fetch.
func newTestApp(t *testing.T, ffmpeg string) (*App, func() []fetched) {
	t.Helper()
	var mu sync.Mutex
	var got []fetched
	a := &App{dataDir: t.TempDir(), findFFmpeg: func(string) string { return ffmpeg }}
	a.dd = doubledouble.New(filepath.Join(a.dataDir, "doubledouble-token"))
	a.sources = source.NewBalancer(time.Minute, fakeSource("lucida"), fakeSource("doubledouble"))
	a.dl = download.NewQueue(func(_ context.Context, _ string, o download.Options, p string, _ func(string)) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, fetched{o, p})
		return p + ".flac", nil
	}, nil, 1)
	t.Cleanup(a.dl.Close)
	return a, func() []fetched {
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
			if !slices.ContainsFunc(a.GetDownloadQueue(), func(i download.Item) bool {
				return i.Status == download.Queued || i.Status == download.Downloading
			}) {
				break
			}
		}
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

func req(format, src string, ids ...string) DownloadRequest {
	r := DownloadRequest{Collection: download.Collection{Kind: "album", ID: "B00A3SGWYC", Name: "A"},
		Options: download.Options{Dir: "D", Format: format, Source: src, FilenameTemplate: "{title}"}}
	for _, id := range ids {
		r.Tracks = append(r.Tracks, download.Track{ID: id, Title: id})
	}
	return r
}

func TestDownloadTracksValidates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ffmpeg  string
		r       DownloadRequest
		wantErr string
	}{
		{"unknown format", "ff", req("wav", "", "B00A3SGXAA"), `unknown download format "wav"`},
		{"unknown source", "ff", req("", "zzz", "B00A3SGXAA"), `unknown download source "zzz"`},
		{"no ffmpeg", "", req("mp3-320", "", "B00A3SGXAA"), "FFmpeg is needed"},
		{"spotify after amazon", "ff", req("", "", "B00A3SGXAA", "6rqhFgbbKwnb9MLmUQDhG6"), "only Amazon Music tracks"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fetches := newTestApp(t, tc.ffmpeg)
			err := a.DownloadTracks(tc.r)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if n := len(a.GetDownloadQueue()); n != 0 || len(fetches()) != 0 {
				t.Fatalf("enqueued %d, fetched %d; want none", n, len(fetches()))
			}
		})
	}

	a, fetches := newTestApp(t, "ff")
	if err := a.DownloadTracks(req("mp3-320", "lucida", "B00A3SGXAA")); err != nil {
		t.Fatal(err)
	}
	if err := a.DownloadTracks(DownloadRequest{}); err != nil {
		t.Fatalf("empty request: %v", err)
	}
	if len(fetches()) != 1 {
		t.Fatalf("fetched %d, want 1", len(fetches()))
	}
}

func TestDownloadTracksDefaults(t *testing.T) {
	a, fetches := newTestApp(t, "")
	r := req("", "", "B00A3SGXAA")
	r.Options.Dir = ""
	if err := a.DownloadTracks(r); err != nil {
		t.Fatal(err)
	}
	got := fetches()
	if len(got) != 1 {
		t.Fatalf("fetched %d, want 1", len(got))
	}
	o := got[0].o
	if o.Source != "auto" || o.Format != "original" || o.Dir != a.DefaultDownloadDir() {
		t.Errorf("options = %+v", o)
	}
	if !strings.HasPrefix(got[0].path, a.DefaultDownloadDir()) {
		t.Errorf("path %q is outside the default dir", got[0].path)
	}
}

func TestGetDownloadQueueNeverNil(t *testing.T) {
	a, _ := newTestApp(t, "")
	if b, _ := json.Marshal(a.GetDownloadQueue()); string(b) != "[]" {
		t.Fatalf("got %s", b)
	}
}

type recSource struct {
	calls int
	path  string
}

func (r *recSource) Name() string { return "lucida" }
func (r *recSource) Download(context.Context, string, string, func(string)) (string, error) {
	r.calls++
	return r.path, nil
}

// TestFetch runs the real fetch through a queue; DownloadTracks is skipped so fetch's own FFmpeg check is hit.
func TestFetch(t *testing.T) {
	for _, tc := range []struct {
		name, format, ffmpeg string
		want                 download.Status
		wantErr              string
		wantCalls            int
	}{
		{"original", "original", "", download.Completed, "", 1},
		{"no ffmpeg", "mp3-320", "", download.Failed, "FFmpeg is needed", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := &recSource{path: filepath.Join(t.TempDir(), "x.flac")}
			a := &App{dataDir: t.TempDir(), findFFmpeg: func(string) string { return tc.ffmpeg }}
			a.sources = source.NewBalancer(time.Minute, src)
			a.dl = download.NewQueue(a.fetch, nil, 1)
			t.Cleanup(a.dl.Close)
			r := req(tc.format, "auto", "B00A3SGXAA")
			r.Options.Dir = t.TempDir()
			a.dl.Enqueue(r.Collection, r.Options, r.Tracks)
			var it download.Item
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
				if q := a.GetDownloadQueue(); len(q) == 1 && q[0].Status != download.Queued && q[0].Status != download.Downloading {
					it = q[0]
					break
				}
			}
			if it.Status != tc.want || !strings.Contains(it.Error, tc.wantErr) || (tc.want == download.Completed && it.Path != src.path) {
				t.Errorf("item = %+v", it)
			}
			if src.calls != tc.wantCalls {
				t.Errorf("source called %d times, want %d", src.calls, tc.wantCalls)
			}
		})
	}
}

// newGuardApp runs the real fetch, balancer and queue (one worker) so the OnLimit wiring is exercised.
func newGuardApp(t *testing.T) *App {
	t.Helper()
	a := &App{dataDir: t.TempDir(), findFFmpeg: func(string) string { return "" }}
	a.sources = source.NewBalancer(time.Minute, fakeSource("lucida"))
	a.sources.OnLimit = a.onLimit
	a.dl = download.NewQueue(a.fetch, nil, 1)
	t.Cleanup(a.dl.Close)
	return a
}

func waitStatus(t *testing.T, a *App, want ...download.Status) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		q := a.GetDownloadQueue()
		if len(q) == len(want) && !slices.ContainsFunc(q, func(i download.Item) bool { return i.Status == download.Downloading }) {
			got := make([]download.Status, len(q))
			for i, it := range q {
				got[i] = it.Status
			}
			if slices.Equal(got, want) {
				return
			}
		}
	}
	t.Fatalf("queue = %+v, want %v", a.GetDownloadQueue(), want)
}

func TestDownloadGuardFlow(t *testing.T) {
	enqueue := func(a *App) {
		t.Helper()
		r := req("original", "lucida", "B00A3SGXA1", "B00A3SGXA2", "B00A3SGXA3")
		r.Options.GuardLimit = 2
		if err := a.DownloadTracks(r); err != nil {
			t.Fatal(err)
		}
	}
	done, queued := download.Completed, download.Queued

	a := newGuardApp(t)
	enqueue(a)
	waitStatus(t, a, done, done, queued)
	time.Sleep(50 * time.Millisecond)
	waitStatus(t, a, done, done, queued) // still held, not drained
	if got, want := a.GetDownloadGuard(), (DownloadGuard{Site: "lucida", Count: 2, Asking: true}); got != want {
		t.Fatalf("guard = %+v, want %+v", got, want)
	}
	a.GuardContinue()
	waitStatus(t, a, done, done, done)
	if got := a.GetDownloadGuard(); got != (DownloadGuard{}) {
		t.Fatalf("guard after continue = %+v", got)
	}

	a = newGuardApp(t)
	enqueue(a)
	waitStatus(t, a, done, done, queued)
	a.GuardWait()
	g := a.GetDownloadGuard()
	if g.Asking || g.Site != "lucida" || g.Count != 2 {
		t.Fatalf("guard after wait = %+v", g)
	}
	if d := time.UnixMilli(g.PausedUntil).Sub(time.Now().Add(guardRest)); d < -time.Minute || d > time.Minute {
		t.Fatalf("PausedUntil off by %v", d)
	}
	if held, _ := a.dl.HeldUntil(); !held {
		t.Fatal("queue not held after GuardWait")
	}
	waitStatus(t, a, done, done, queued)

	// An expired pause clears itself.
	a.guardMu.Lock()
	a.guard.PausedUntil = time.Now().Add(-time.Second).UnixMilli()
	a.guardMu.Unlock()
	if got := a.GetDownloadGuard(); got != (DownloadGuard{}) {
		t.Fatalf("expired guard = %+v", got)
	}
}

func TestDownloadTracksRejectsNegativeGuardLimit(t *testing.T) {
	a, _ := newTestApp(t, "")
	r := req("", "", "B00A3SGXAA")
	r.Options.GuardLimit = -1
	if err := a.DownloadTracks(r); err == nil || !strings.Contains(err.Error(), "guard limit must be 0 or more") {
		t.Fatalf("err = %v", err)
	}
}

func TestDownloadTracksFixedDoubleDoubleNeedsToken(t *testing.T) {
	a, fetches := newTestApp(t, "ff")
	err := a.DownloadTracks(req("", "doubledouble", "B00A3SGXAA"))
	if err == nil || err.Error() != "pass the doubledouble check in Settings → Downloads first" {
		t.Fatalf("err = %v", err)
	}
	if n := len(a.GetDownloadQueue()); n != 0 || len(fetches()) != 0 {
		t.Fatalf("enqueued %d, fetched %d; want none", n, len(fetches()))
	}
	a.dd.SetToken("t")
	if err := a.DownloadTracks(req("", "doubledouble", "B00A3SGXAA")); err != nil {
		t.Fatalf("with a token: %v", err)
	}
	if err := a.DownloadTracks(req("", "lucida", "B00A3SGXAB")); err != nil {
		t.Fatalf("lucida never needs the token: %v", err)
	}
}

func TestInitMarksDoubleDoubleNeedsCheckWithoutToken(t *testing.T) {
	for _, withToken := range []bool{false, true} {
		dir := t.TempDir()
		if withToken {
			os.WriteFile(filepath.Join(dir, "doubledouble-token"), []byte("t"), 0o600)
		}
		a := &App{dataDir: dir}
		a.init()
		st := a.GetSourceStatus()
		a.shutdown(nil)
		var dd source.Status
		for _, s := range st {
			if s.Name == "doubledouble" {
				dd = s
			}
		}
		if dd.NeedsCheck == withToken || dd.Available != withToken {
			t.Errorf("token=%v: status %+v", withToken, dd)
		}
	}
}

func TestShutdownCancelsRunningCheckAndWaits(t *testing.T) {
	a := &App{}
	started := make(chan struct{})
	var returned atomic.Bool
	errc := make(chan error, 1)
	go func() {
		errc <- a.runCheck(func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			time.Sleep(20 * time.Millisecond) // the browser takes a moment to go
			returned.Store(true)
			return ctx.Err()
		})
	}()
	<-started
	a.stopCheck(5 * time.Second)
	if !returned.Load() {
		t.Fatal("stopCheck returned before the check did")
	}
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
	a.stopCheck(time.Millisecond) // nothing running: must not block or panic
}

func TestRunCheckMapsTimeoutAndRejectsSecond(t *testing.T) {
	a := &App{}
	err := a.runCheck(func(context.Context) error { return fmt.Errorf("x: %w", context.DeadlineExceeded) })
	if err == nil || err.Error() != "the doubledouble check timed out" {
		t.Fatalf("err = %v", err)
	}
	release := make(chan struct{})
	started := make(chan struct{})
	go a.runCheck(func(context.Context) error { close(started); <-release; return nil })
	<-started
	if err := a.runCheck(func(context.Context) error { return nil }); err == nil {
		t.Error("second concurrent check was allowed")
	}
	close(release)
}

func TestExistingDir(t *testing.T) {
	dir := t.TempDir()
	if got := existingDir(filepath.Join(dir, "missing", "deeper")); got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
	if got := existingDir(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
