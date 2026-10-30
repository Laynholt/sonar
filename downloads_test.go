package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	"sonar/internal/genre"
	"sonar/internal/history"
	"sonar/internal/match"
	"sonar/internal/model"
	"sonar/internal/resolve"
	"sonar/internal/source"
)

type fakeSource string

func (f fakeSource) Name() string { return string(f) }
func (f fakeSource) Download(context.Context, string, string, func(string)) (string, error) {
	return "", nil
}

// nopFinder finds nothing, so every Spotify track lands in review with no candidates.
type nopFinder struct{}

func nopFinders(string, string) (resolve.Finder, error) { return nopFinder{}, nil }

func (nopFinder) Search(context.Context, string) ([]match.Item, error) { return nil, nil }
func (nopFinder) LookupTracks(context.Context, []string) ([]model.TrackInfo, error) {
	return nil, nil
}
func (nopFinder) LookupAlbum(context.Context, string) (*model.AlbumInfo, error) { return nil, nil }
func (nopFinder) OriginIDsByISRC(context.Context, string) ([]string, error)     { return nil, nil }

// newQueueApp wires an App on dir the way init does, with fake sites and a finder that finds nothing.
// A nil fetch uses the App's real fetch.
func newQueueApp(t *testing.T, dir string, fetch download.Fetch, sites ...legacySite) *App {
	t.Helper()
	a := &App{dataDir: dir, findFFmpeg: func(string) string { return "" }}
	a.dd = doubledouble.New(filepath.Join(dir, "doubledouble-token"))
	if fetch == nil {
		fetch = amazonFetch(a.fetch) // shim until Task 13
	}
	a.startQueue(fetch, legacyBalancer(time.Minute, sites...), nopFinders)
	t.Cleanup(func() { a.shutdown(nil) })
	return a
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
	a := newQueueApp(t, t.TempDir(), func(_ context.Context, _ string, _ download.Track, _ map[string]string, o download.Options, p string, _ func(string)) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, fetched{o, p})
		return p + ".flac", nil
	}, fakeSource("lucida"), fakeSource("doubledouble"))
	a.findFFmpeg = func(string) string { return ffmpeg }
	return a, func() []fetched {
		waitFor(t, func() bool {
			return !slices.ContainsFunc(a.GetDownloadState().Items, func(i download.Item) bool {
				return i.Status == download.Queued || i.Status == download.Downloading || i.Status == download.Matching
			})
		})
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met in 5s")
}

func req(format, src string, ids ...string) DownloadRequest {
	r := DownloadRequest{Collection: download.Collection{Kind: "album", ID: "B00A3SGWYC", Name: "A"},
		Options: download.Options{Dir: "D", Format: format, Source: src, FilenameTemplate: "{title}"}}
	for _, id := range ids {
		r.Tracks = append(r.Tracks, download.Track{ID: id, Title: id})
	}
	return r
}

func spotifyReq(kind string, tracks ...download.Track) DownloadRequest {
	return DownloadRequest{Collection: download.Collection{Kind: kind, ID: "x", Name: "A"},
		Options: download.Options{Dir: "D", FilenameTemplate: "{title}"}, Tracks: tracks}
}

func TestDownloadTracksValidates(t *testing.T) {
	withService := func(r DownloadRequest, service string) DownloadRequest {
		r.Tracks[0].Service = service
		return r
	}
	withWait := func(r DownloadRequest, wait int) DownloadRequest {
		r.Options.GuardWait = wait
		return r
	}
	for _, tc := range []struct {
		name    string
		ffmpeg  string
		r       DownloadRequest
		wantErr string
	}{
		{"unknown format", "ff", req("wav", "", "B00A3SGXAA"), `unknown download format "wav"`},
		{"unknown source", "ff", req("", "zzz", "B00A3SGXAA"), `unknown download source "zzz"`},
		{"no ffmpeg", "", req("mp3-320", "", "B00A3SGXAA"), "FFmpeg is needed"},
		{"bad amazon id", "ff", req("", "", "B00A3SGXAA", "nope"), `not an Amazon Music track ID: "nope"`},
		{"spotify id as amazon", "ff", req("", "", "6rqhFgbbKwnb9MLmUQDhG6"), "not an Amazon Music track ID"},
		{"bad spotify id", "ff", withService(req("", "", "B00A3SGXAA"), "spotify"), `not a Spotify track ID: "B00A3SGXAA"`},
		{"unknown service", "ff", withService(req("", "", "B00A3SGXAA"), "tidal"), `unknown service "tidal"`},
		{"guard wait 45", "ff", withWait(req("", "", "B00A3SGXAA"), 45), "guard wait must be 15, 30, 60 or 120 minutes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fetches := newTestApp(t, tc.ffmpeg)
			err := a.DownloadTracks(tc.r)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}
			if n := len(a.GetDownloadState().Items); n != 0 || len(fetches()) != 0 {
				t.Fatalf("enqueued %d, fetched %d; want none", n, len(fetches()))
			}
		})
	}

	a, fetches := newTestApp(t, "ff")
	if err := a.DownloadTracks(withService(req("mp3-320", "lucida", "B00A3SGXAA"), "amazon")); err != nil {
		t.Fatal(err)
	}
	if err := a.DownloadTracks(DownloadRequest{}); err != nil {
		t.Fatalf("empty request: %v", err)
	}
	if len(fetches()) != 1 {
		t.Fatalf("fetched %d, want 1", len(fetches()))
	}
}

func TestDownloadTracksAcceptsSpotify(t *testing.T) {
	sp := func(id, album string) download.Track {
		return download.Track{ID: id, Title: id, Service: "spotify", AlbumID: album}
	}
	for _, tc := range []struct {
		kind   string
		tracks []download.Track
		groups []string
	}{
		// A bad Spotify ID is skipped, not fatal to the rest.
		{"album", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1"), sp("nope", "al1"), sp("4uLU6hMCjMI75M1A2tKUQC", "al1")}, []string{"album:al1", "album:al1"}},
		{"artist", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1"), sp("4uLU6hMCjMI75M1A2tKUQC", "al2")}, []string{"album:al1", "album:al2"}},
		{"playlist", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1"), sp("4uLU6hMCjMI75M1A2tKUQC", "al2")}, []string{"", ""}},
		{"track", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1")}, []string{""}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			a, _ := newTestApp(t, "")
			if err := a.DownloadTracks(spotifyReq(tc.kind, tc.tracks...)); err != nil {
				t.Fatal(err)
			}
			items := a.GetDownloadState().Items
			var groups []string
			for _, it := range items {
				groups = append(groups, it.Track.Group)
				if it.IDs["amazon"] != "" {
					t.Errorf("%s: a Spotify track got an Amazon ID %q before matching", it.ID, it.IDs["amazon"])
				}
			}
			if !slices.Equal(groups, tc.groups) {
				t.Errorf("groups = %q, want %q", groups, tc.groups)
			}
			if tc.tracks[0].Group != "" {
				t.Error("DownloadTracks changed the caller's tracks")
			}
		})
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
	if o.Source != "auto" || o.Format != "original" || o.Dir != a.DefaultDownloadDir() || o.GuardWait != 30 {
		t.Errorf("options = %+v", o)
	}
	if !strings.HasPrefix(got[0].path, a.DefaultDownloadDir()) {
		t.Errorf("path %q is outside the default dir", got[0].path)
	}
}

func TestGetDownloadStateItemsNeverNil(t *testing.T) {
	a, _ := newTestApp(t, "")
	b, _ := json.Marshal(a.GetDownloadState())
	if !strings.Contains(string(b), `"items":[]`) || !strings.Contains(string(b), `"held":false`) {
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
			a := newQueueApp(t, t.TempDir(), nil, src)
			a.findFFmpeg = func(string) string { return tc.ffmpeg }
			r := req(tc.format, "auto", "B00A3SGXAA")
			r.Options.Dir = t.TempDir()
			a.dl.Enqueue(r.Collection, r.Options, r.Tracks)
			var it download.Item
			waitFor(t, func() bool {
				q := a.GetDownloadState().Items
				if len(q) == 1 && q[0].Status != download.Queued && q[0].Status != download.Downloading {
					it = q[0]
					return true
				}
				return false
			})
			if it.Status != tc.want || !strings.Contains(it.Error, tc.wantErr) || (tc.want == download.Completed && it.Path != src.path) {
				t.Errorf("item = %+v", it)
			}
			if src.calls != tc.wantCalls {
				t.Errorf("source called %d times, want %d", src.calls, tc.wantCalls)
			}
		})
	}
}

type downSource struct{ err error }

func (downSource) Name() string { return "lucida" }
func (d downSource) Download(context.Context, string, string, func(string)) (string, error) {
	return "", d.err
}

func TestFetchMarksSiteDown(t *testing.T) {
	for _, tc := range []struct {
		err  error
		down bool
	}{{source.ErrUnavailable, true}, {source.ErrNeedsCheck, true}, {errors.New("no stream"), false}} {
		a := &App{findFFmpeg: func(string) string { return "" }}
		a.sources = legacyBalancer(time.Minute, downSource{fmt.Errorf("x: %w", tc.err)})
		_, err := a.fetch(context.Background(), "lucida", download.Track{}, "B00A3SGXAA", download.Options{Format: "original"}, t.TempDir()+"/x", nil)
		if errors.Is(err, download.ErrSiteDown) != tc.down || !errors.Is(err, tc.err) {
			t.Errorf("%v: err = %v", tc.err, err)
		}
	}
}

func TestFetchNoFFmpegErrorHasNoSentinel(t *testing.T) {
	a := &App{findFFmpeg: func(string) string { return "" }}
	_, err := a.fetch(context.Background(), "lucida", download.Track{}, "B00A3SGXAA", download.Options{Format: "mp3-320"}, t.TempDir()+"/x", nil)
	if err == nil || strings.Contains(err.Error(), "NO_FFMPEG") || !strings.HasPrefix(err.Error(), "FFmpeg is needed") {
		t.Errorf("err = %v", err)
	}
}

func waitStatus(t *testing.T, a *App, want ...download.Status) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		q := a.GetDownloadState().Items
		got := make([]download.Status, len(q))
		for i, it := range q {
			got[i] = it.Status
		}
		if slices.Equal(got, want) {
			return
		}
	}
	t.Fatalf("queue = %+v, want %v", a.GetDownloadState().Items, want)
}

func site(a *App, name string) source.Status {
	for _, s := range a.GetDownloadState().Sites {
		if s.Name == name {
			return s
		}
	}
	return source.Status{}
}

// TestDownloadGuardFlow runs the real fetch, balancer and queue: lucida asks at the limit while doubledouble does not.
func TestDownloadGuardFlow(t *testing.T) {
	enqueue := func(a *App, wait int) {
		t.Helper()
		r := req("original", "lucida", "B00A3SGXA1", "B00A3SGXA2", "B00A3SGXA3")
		r.Options.GuardLimit, r.Options.GuardWait = 2, wait
		if err := a.DownloadTracks(r); err != nil {
			t.Fatal(err)
		}
	}
	done, queued := download.Completed, download.Queued
	newApp := func() *App {
		a := newQueueApp(t, t.TempDir(), nil, fakeSource("lucida"), fakeSource("doubledouble"))
		a.dd.SetToken("t")
		return a
	}

	a := newApp()
	enqueue(a, 0)
	waitStatus(t, a, done, done, queued)
	time.Sleep(50 * time.Millisecond)
	waitStatus(t, a, done, done, queued) // lucida asks; doubledouble may not take a fixed-source track
	if s := site(a, "lucida"); !s.Asking || s.Count != 2 || s.Limit != 2 || s.PausedUntil != 0 {
		t.Fatalf("lucida = %+v", s)
	}
	if s := site(a, "doubledouble"); s.Asking {
		t.Fatalf("doubledouble = %+v", s)
	}
	if a.GetDownloadState().Held {
		t.Fatal("the guard must not hold the queue")
	}
	a.GuardContinue("doubledouble") // not asking: no effect
	waitStatus(t, a, done, done, queued)
	a.GuardContinue("lucida")
	waitStatus(t, a, done, done, done)
	if s := site(a, "lucida"); s.Asking || s.PausedUntil != 0 {
		t.Fatalf("lucida after continue = %+v", s)
	}

	a = newApp()
	enqueue(a, 60)
	waitStatus(t, a, done, done, queued)
	waitFor(t, func() bool { return site(a, "lucida").Asking })
	if err := a.GuardWait("lucida", 60); err != nil {
		t.Fatal(err)
	}
	s := site(a, "lucida")
	if s.Asking || s.Count != 2 {
		t.Fatalf("lucida after wait = %+v", s)
	}
	if d := time.UnixMilli(s.PausedUntil).Sub(time.Now().Add(60 * time.Minute)); d < -time.Minute || d > time.Minute {
		t.Fatalf("PausedUntil off by %v", d)
	}
	time.Sleep(50 * time.Millisecond)
	waitStatus(t, a, done, done, queued)
	a.GuardContinue("lucida") // "Resume now"
	waitStatus(t, a, done, done, done)
	if s := site(a, "lucida"); s.Asking || s.PausedUntil != 0 {
		t.Fatalf("lucida after resume = %+v", s)
	}
}

func TestGuardWaitValidation(t *testing.T) {
	a := newQueueApp(t, t.TempDir(), nil, fakeSource("lucida"))
	r := req("original", "lucida", "B00A3SGXA1")
	r.Options.GuardLimit, r.Options.GuardWait = 1, 45
	if err := a.DownloadTracks(r); err == nil {
		t.Fatal("45 minutes was accepted")
	}
	r.Options.GuardWait = 0
	if err := a.DownloadTracks(r); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return site(a, "lucida").Asking })
	if err := a.GuardWait("lucida", 45); err == nil || err.Error() != "guard wait must be 15, 30, 60 or 120 minutes" {
		t.Fatalf("GuardWait 45: err = %v", err)
	}
	if s := site(a, "lucida"); !s.Asking || s.PausedUntil != 0 {
		t.Fatalf("a rejected wait changed lucida: %+v", s)
	}
	if err := a.GuardWait("lucida", 0); err != nil {
		t.Fatal(err)
	}
	if d := time.UnixMilli(site(a, "lucida").PausedUntil).Sub(time.Now().Add(30 * time.Minute)); d < -time.Minute || d > time.Minute {
		t.Fatalf("0 should mean 30 minutes; off by %v", d)
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
	if n := len(a.GetDownloadState().Items); n != 0 || len(fetches()) != 0 {
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

// reviewApp has one Spotify track in review, with no candidates.
func reviewApp(t *testing.T, dir string) *App {
	t.Helper()
	a := newQueueApp(t, dir, nil, fakeSource("lucida"))
	tr := download.Track{ID: "6rqhFgbbKwnb9MLmUQDhG6", Title: "T", Service: "spotify", AlbumID: "al1"}
	if err := a.DownloadTracks(spotifyReq("album", tr)); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, a, download.Review)
	return a
}

func TestChooseCandidateCachesOnlyAfterChoose(t *testing.T) {
	const id = "6rqhFgbbKwnb9MLmUQDhG6"
	a := reviewApp(t, t.TempDir())
	if err := a.ChooseCandidate("4uLU6hMCjMI75M1A2tKUQC", "B00A3SGXAA"); err == nil {
		t.Fatal("chose for a track that is not in review")
	}
	if _, _, _, ok := a.cache.Get("spotify", "amazon", "4uLU6hMCjMI75M1A2tKUQC"); ok {
		t.Fatal("a failed choice was cached")
	}
	if err := a.ChooseCandidate(id, "not-an-asin"); err == nil {
		t.Fatal("a bad Amazon ID was accepted")
	}
	if _, _, _, ok := a.cache.Get("spotify", "amazon", id); ok {
		t.Fatal("a bad Amazon ID was cached")
	}
	if err := a.ChooseCandidate(id, "B00A3SGXAA"); err != nil {
		t.Fatal(err)
	}
	if amz, via, _, _ := a.cache.Get("spotify", "amazon", id); amz != "B00A3SGXAA" || via != "manual" {
		t.Fatalf("cache = %q, %q", amz, via)
	}
	waitStatus(t, a, download.Completed)
}

func TestUseAmazonLinkRejectsAlbum(t *testing.T) {
	const id = "6rqhFgbbKwnb9MLmUQDhG6"
	a := reviewApp(t, t.TempDir())
	for _, link := range []string{"https://music.amazon.com/albums/B00A3SGWYC", "https://example.com/x"} {
		if err := a.UseAmazonLink(id, link); err == nil || err.Error() != "paste a link to an Amazon Music track" {
			t.Fatalf("%s: err = %v", link, err)
		}
	}
	waitStatus(t, a, download.Review)
	if err := a.UseAmazonLink(id, "https://music.amazon.com/albums/B00A3SGWYC?trackAsin=B00A3SGXAB"); err != nil {
		t.Fatal(err)
	}
	if amz, via, _, _ := a.cache.Get("spotify", "amazon", id); amz != "B00A3SGXAB" || via != "manual" {
		t.Fatalf("cache = %q, %q", amz, via)
	}
}

func TestSkipReview(t *testing.T) {
	a := reviewApp(t, t.TempDir())
	a.SkipReview("6rqhFgbbKwnb9MLmUQDhG6")
	waitStatus(t, a, download.NotFound)
}

func TestQueueSavedAndRestored(t *testing.T) {
	dir := t.TempDir()
	a := reviewApp(t, dir)
	if b, _ := json.Marshal(a.GetPendingQueue()); string(b) != `{"tracks":0,"review":0,"collections":[]}` {
		t.Fatalf("pending while not held = %s", b)
	}
	waitFor(t, func() bool { // the saver writes it, without a shutdown
		s, _ := download.LoadState(filepath.Join(dir, "queue.json"))
		return len(s.Items) == 1 && s.Items[0].Item.Status == download.Review
	})

	b := newQueueApp(t, dir, nil, fakeSource("lucida"))
	if p := b.GetPendingQueue(); p.Tracks != 1 || p.Review != 1 || !slices.Equal(p.Collections, []string{"A"}) {
		t.Fatalf("pending = %+v", p)
	}
	if !b.GetDownloadState().Held {
		t.Fatal("restored queue is not held")
	}
	b.ResumeQueue()
	if b.GetDownloadState().Held || b.GetPendingQueue().Tracks != 0 {
		t.Fatal("ResumeQueue left the queue held")
	}
	b.ClearQueue()
	if n := len(b.GetDownloadState().Items); n != 0 {
		t.Fatalf("%d items left after ClearQueue", n)
	}
}

func TestClearQueueReleasesHold(t *testing.T) {
	dir := t.TempDir()
	reviewApp(t, dir).shutdown(nil)
	b := newQueueApp(t, dir, nil, fakeSource("lucida"))
	b.ClearQueue()
	if st := b.GetDownloadState(); st.Held || len(st.Items) != 0 {
		t.Fatalf("state = %+v", st)
	}
}

func TestCorruptQueueFileStartsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "queue.json")
	if err := os.WriteFile(path, []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newQueueApp(t, dir, nil, fakeSource("lucida"))
	if st := a.GetDownloadState(); st.Held || len(st.Items) != 0 {
		t.Fatalf("state = %+v", st)
	}
	time.Sleep(50 * time.Millisecond)
	if raw, _ := os.ReadFile(path); string(raw) != "{nope" {
		t.Fatalf("queue.json was rewritten before any change: %q", raw)
	}
}

func TestShutdownSavesBeforeClose(t *testing.T) {
	dir := t.TempDir()
	started := make(chan struct{})
	a := newQueueApp(t, dir, func(ctx context.Context, _ string, _ download.Track, _ map[string]string, _ download.Options, _ string, _ func(string)) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}, fakeSource("lucida"))
	if err := a.DownloadTracks(req("original", "", "B00A3SGXAA")); err != nil {
		t.Fatal(err)
	}
	<-started
	path := filepath.Join(dir, "queue.json")
	waitFor(t, func() bool { // the saver has written the downloading item and has nothing left to do
		s, _ := download.LoadState(path)
		return len(s.Items) == 1
	})
	if err := os.Remove(path); err != nil { // so only shutdown's own save can bring it back
		t.Fatal(err)
	}
	a.shutdown(nil)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shutdown did not save: %v", err)
	}
	s, err := download.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Items) != 1 || s.Items[0].Item.Status != download.Queued {
		t.Fatalf("saved = %+v, want one queued item", s.Items)
	}
}

func TestGuardPauseSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	a := newQueueApp(t, dir, nil, fakeSource("lucida"))
	r := req("original", "lucida", "B00A3SGXA1")
	r.Options.GuardLimit = 1
	if err := a.DownloadTracks(r); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return site(a, "lucida").Asking })
	if err := a.GuardWait("lucida", 30); err != nil {
		t.Fatal(err)
	}
	until := site(a, "lucida").PausedUntil
	if until == 0 {
		t.Fatal("no guard pause")
	}
	a.shutdown(nil)

	b := newQueueApp(t, dir, nil, fakeSource("lucida"))
	if got := site(b, "lucida"); got.PausedUntil != until || got.Count != 0 {
		t.Fatalf("restored lucida = %+v, want PausedUntil %d and Count 0", got, until)
	}
	if b.sources.Ready("lucida") {
		t.Fatal("lucida is ready during its guard pause")
	}
}

// TestGuardWaitTakesTheDialogsMinutes restores a queue saved with a 120-minute wait: "Wait" pauses for the minutes it is given.
func TestGuardWaitTakesTheDialogsMinutes(t *testing.T) {
	dir := t.TempDir()
	tr := download.Track{ID: "B00A3SGXA1", Title: "T"}
	err := download.SaveState(filepath.Join(dir, "queue.json"), download.State{Version: 1, Items: []download.Saved{{
		Item:       download.Item{ID: tr.ID, Track: tr, Status: download.Queued},
		Collection: download.Collection{Kind: "track", ID: tr.ID, Name: "T"},
		Options:    download.Options{Dir: t.TempDir(), Format: "original", Source: "lucida", FilenameTemplate: "{title}", GuardLimit: 1, GuardWait: 120},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	a := newQueueApp(t, dir, nil, fakeSource("lucida"))
	a.ResumeQueue()
	waitFor(t, func() bool { return site(a, "lucida").Asking })
	if err := a.GuardWait("lucida", 15); err != nil {
		t.Fatal(err)
	}
	if d := time.UnixMilli(site(a, "lucida").PausedUntil).Sub(time.Now().Add(15 * time.Minute)); d < -time.Minute || d > time.Minute {
		t.Fatalf("want a 15-minute pause; off by %v", d)
	}
}

// A second instance finds history.db locked: it must neither restore nor rewrite the first one's queue.json.
func TestSecondInstanceLeavesQueueFileAlone(t *testing.T) {
	dir := t.TempDir()
	h, err := history.Open(filepath.Join(dir, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	tr := download.Track{ID: "B00A3SGXA1", Title: "T"}
	path := filepath.Join(dir, "queue.json")
	err = download.SaveState(path, download.State{Version: 1, Items: []download.Saved{{
		Item:       download.Item{ID: tr.ID, Track: tr, Status: download.Queued},
		Collection: download.Collection{Kind: "track", ID: tr.ID, Name: "T"},
		Options:    download.Options{Dir: t.TempDir(), Format: "original", FilenameTemplate: "{title}"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)

	a := &App{dataDir: dir}
	a.init()
	if a.hist != nil {
		t.Fatal("the second instance opened history")
	}
	if st := a.GetDownloadState(); st.Held || len(st.Items) != 0 {
		t.Fatalf("the second instance restored the queue: %+v", st)
	}
	a.ClearQueue() // a change the first instance would save
	time.Sleep(50 * time.Millisecond)
	a.shutdown(nil)
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatalf("queue.json was rewritten: %s", after)
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
		if dd.NeedsAction == withToken || dd.Available != withToken {
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

// fnSource is a download site whose Download is fn.
type fnSource func(ctx context.Context, pathNoExt string) (string, error)

func (fnSource) Name() string { return "lucida" }
func (f fnSource) Download(ctx context.Context, _, pathNoExt string, _ func(string)) (string, error) {
	return f(ctx, pathNoExt)
}

func flacSource(_ context.Context, p string) (string, error) { return p + ".flac", nil }

// swap sets *p to v until the test ends.
func swap[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

type genreWrite struct{ path, genre string }

// fakeWriteGenre replaces writeGenre; the returned func gives the calls so far.
func fakeWriteGenre(t *testing.T, err error) func() []genreWrite {
	var mu sync.Mutex
	var got []genreWrite
	swap(t, &writeGenre, func(_ context.Context, _, path, g string) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, genreWrite{path, g})
		return err
	})
	return func() []genreWrite { mu.Lock(); defer mu.Unlock(); return slices.Clone(got) }
}

var genreTrack = download.Track{ID: "6rqhFgbbKwnb9MLmUQDhG6", Title: "T", Service: "spotify"}

func genreOpts(single bool) download.Options {
	return download.Options{Format: "original", EmbedGenre: true, SingleGenre: single}
}

// fetchGenre runs a.fetch on genreTrack and returns the path, the statuses and the error.
func fetchGenre(t *testing.T, a *App, o download.Options) (string, []string, error) {
	t.Helper()
	var st []string
	p, err := a.fetch(context.Background(), "lucida", genreTrack, "B00A3SGXAA", o, t.TempDir()+"/x", func(m string) { st = append(st, m) })
	return p, st, err
}

func TestFetchWritesGenreFLAC(t *testing.T) {
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	a.genres = func(context.Context, download.Track, string) ([]string, error) { return []string{"rap", "trap"}, nil }
	writes := fakeWriteGenre(t, nil)
	p, _, err := fetchGenre(t, a, genreOpts(false))
	if err != nil {
		t.Fatal(err)
	}
	if w := writes(); !slices.Equal(w, []genreWrite{{p, "Rap, Trap"}}) {
		t.Errorf("writes = %v, path %q", w, p)
	}
	writes = fakeWriteGenre(t, nil)
	if _, _, err := fetchGenre(t, a, genreOpts(true)); err != nil || len(writes()) != 1 || writes()[0].genre != "Rap" {
		t.Errorf("single: %v, %v", writes(), err)
	}
}

func TestFetchGenreWriteErrorKeepsTrack(t *testing.T) {
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	a.genres = func(context.Context, download.Track, string) ([]string, error) { return []string{"rap"}, nil }
	fakeWriteGenre(t, errors.New("disk"))
	if p, _, err := fetchGenre(t, a, genreOpts(false)); err != nil || p == "" {
		t.Errorf("path %q, err %v", p, err)
	}
}

func TestFetchGenreOverlapsDownload(t *testing.T) {
	siteStarted, lookupStarted := make(chan struct{}), make(chan struct{})
	wait := func(c chan struct{}) error {
		select {
		case <-c:
			return nil
		case <-time.After(3 * time.Second):
			return errors.New("no overlap")
		}
	}
	a := newQueueApp(t, t.TempDir(), nil, fnSource(func(_ context.Context, p string) (string, error) {
		close(siteStarted)
		if err := wait(lookupStarted); err != nil { // the lookup must start before the site returns
			return "", err
		}
		return p + ".flac", nil
	}))
	a.genres = func(context.Context, download.Track, string) ([]string, error) {
		close(lookupStarted)
		return []string{"rap"}, wait(siteStarted)
	}
	writes := fakeWriteGenre(t, nil)
	if _, _, err := fetchGenre(t, a, genreOpts(false)); err != nil {
		t.Fatal(err)
	}
	if len(writes()) != 1 {
		t.Errorf("writes = %v", writes())
	}
}

func TestFetchGenreTimeoutStillCompletes(t *testing.T) {
	swap(t, &genreBudget, 20*time.Millisecond)
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })
	a.genres = func(context.Context, download.Track, string) ([]string, error) { <-hang; return []string{"rap"}, nil }
	writes := fakeWriteGenre(t, nil)
	start := time.Now()
	p, st, err := fetchGenre(t, a, genreOpts(false))
	if err != nil || !strings.HasSuffix(p, ".flac") {
		t.Fatalf("path %q, err %v", p, err)
	}
	if time.Since(start) > 2*time.Second || len(writes()) != 0 || !slices.Contains(st, "Looking up genre…") {
		t.Errorf("took %v, writes %v, statuses %q", time.Since(start), writes(), st)
	}
}

func TestFetchGenreCancelsLookupOnReturn(t *testing.T) {
	a := newQueueApp(t, t.TempDir(), nil, fnSource(func(context.Context, string) (string, error) { return "", errors.New("boom") }))
	done := make(chan struct{})
	a.genres = func(ctx context.Context, _ download.Track, _ string) ([]string, error) {
		defer close(done)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if _, _, err := fetchGenre(t, a, genreOpts(false)); err == nil {
		t.Fatal("want the site error")
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("the lookup outlived fetch")
	}
}

func TestFetchGenreErrorNoGenre(t *testing.T) {
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	a.genres = func(context.Context, download.Track, string) ([]string, error) { return nil, errors.New("chosic down") }
	writes := fakeWriteGenre(t, nil)
	if p, _, err := fetchGenre(t, a, genreOpts(false)); err != nil || p == "" || len(writes()) != 0 {
		t.Errorf("path %q, err %v, writes %v", p, err, writes())
	}
}

func TestFetchGenreDisabled(t *testing.T) {
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	var calls atomic.Int32
	a.genres = func(context.Context, download.Track, string) ([]string, error) {
		calls.Add(1)
		return []string{"rap"}, nil
	}
	writes := fakeWriteGenre(t, nil)
	o := genreOpts(false)
	o.EmbedGenre = false
	if _, st, err := fetchGenre(t, a, o); err != nil || calls.Load() != 0 || len(writes()) != 0 || len(st) != 0 {
		t.Errorf("err %v, lookups %d, writes %v, statuses %q", err, calls.Load(), writes(), st)
	}
}

// fakeChosic serves one artist with genres "rap" and "trap" and records the track IDs asked for.
func fakeChosic(t *testing.T) (*httptest.Server, func() []string) {
	var mu sync.Mutex
	var ids []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			w.Write([]byte(`{"success":true}`))
		case strings.HasPrefix(r.URL.Path, "/api/tools/tracks/"):
			mu.Lock()
			ids = append(ids, strings.TrimPrefix(r.URL.Path, "/api/tools/tracks/"))
			mu.Unlock()
			w.Write([]byte(`{"artists":[{"id":"a1"}]}`))
		default:
			w.Write([]byte(`{"genres":["rap","trap"]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(ids) }
}

func TestFetchGenreCachedAcrossRetry(t *testing.T) {
	srv, asked := fakeChosic(t)
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	a.genreClient = genre.Client{Base: srv.URL, HTTP: srv.Client()}
	writes := fakeWriteGenre(t, nil)
	for range 2 {
		if _, _, err := fetchGenre(t, a, genreOpts(false)); err != nil {
			t.Fatal(err)
		}
	}
	if got := asked(); !slices.Equal(got, []string{genreTrack.ID}) {
		t.Errorf("chosic asked %q, want once", got)
	}
	if w := writes(); len(w) != 2 || w[1].genre != "Rap, Trap" {
		t.Errorf("writes = %v", w)
	}
}

func TestFetchGenreAmazonViaISRC(t *testing.T) {
	srv, asked := fakeChosic(t)
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	a.genreClient = genre.Client{Base: srv.URL, HTTP: srv.Client()}
	swap(t, &amazonLookup, func(_ context.Context, ids []string) ([]model.TrackInfo, error) {
		if !slices.Equal(ids, []string{"B00A3SGXAA"}) {
			t.Errorf("lookup ids = %q", ids)
		}
		return []model.TrackInfo{{ID: ids[0], ISRC: "USRC17607839"}}, nil
	})
	swap(t, &spotifyByISRC, func(_ context.Context, isrc string) ([]string, error) {
		if isrc != "USRC17607839" {
			t.Errorf("isrc = %q", isrc)
		}
		return []string{"0VjIjW4GlUZAMYd2vXMi3b", "other"}, nil
	})
	writes := fakeWriteGenre(t, nil)
	tr := download.Track{ID: "B00A3SGXAA", Title: "T"} // Service "" means amazon
	if _, err := a.fetch(context.Background(), "lucida", tr, "B00A3SGXAA", genreOpts(false), t.TempDir()+"/x", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if got := asked(); !slices.Equal(got, []string{"0VjIjW4GlUZAMYd2vXMi3b"}) {
		t.Errorf("chosic asked %q", got)
	}
	if w := writes(); len(w) != 1 || w[0].genre != "Rap, Trap" {
		t.Errorf("writes = %v", w)
	}
}

func TestFetchConvertWritesGenreAfter(t *testing.T) {
	a := newQueueApp(t, t.TempDir(), nil, fnSource(flacSource))
	a.findFFmpeg = func(string) string { return "ff" }
	a.genres = func(context.Context, download.Track, string) ([]string, error) { return []string{"rap", "trap"}, nil }
	writes := fakeWriteGenre(t, nil)
	var gotIn string
	swap(t, &convertFile, func(_ context.Context, _, in, _ string) (string, error) {
		gotIn = in
		return "out.m4a", nil
	})
	o := genreOpts(false)
	o.Format = "m4a-aac-320"
	p, _, err := fetchGenre(t, a, o)
	if err != nil || p != "out.m4a" || !strings.HasSuffix(gotIn, ".flac") || !slices.Equal(writes(), []genreWrite{{"out.m4a", "Rap, Trap"}}) {
		t.Errorf("path %q, err %v, in %q, writes %v", p, err, gotIn, writes())
	}
}
