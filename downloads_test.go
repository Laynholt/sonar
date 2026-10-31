package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"sonar/internal/download"
	"sonar/internal/history"
	"sonar/internal/source"
)

type fetched struct {
	o    download.Options
	path string
}

// newTestApp builds an App with today's fake plugins whose queue records what it was asked to fetch.
func newTestApp(t *testing.T, ffmpeg string) (*App, func() []fetched) {
	t.Helper()
	var mu sync.Mutex
	var got []fetched
	a := newPluginApp(t, t.TempDir(), func(_ context.Context, _ string, _ download.Track, _ map[string]string, o download.Options, p string, _ func(string)) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, fetched{o, p})
		return p + ".flac", nil
	}, stdPlugs("")...)
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
	// 15s: plugin processes start and match slowly under -race on a loaded machine.
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met in 15s")
}

// req asks for Amazon tracks ids into a temp folder.
func req(t *testing.T, format, src string, ids ...string) DownloadRequest {
	r := DownloadRequest{Collection: download.Collection{Kind: "album", ID: "B00A3SGWYC", Name: "A"},
		Options: download.Options{Dir: t.TempDir(), Format: format, Source: src, FilenameTemplate: "{title}"}}
	for _, id := range ids {
		r.Tracks = append(r.Tracks, download.Track{ID: id, Title: id})
	}
	return r
}

func spotifyReq(t *testing.T, kind string, tracks ...download.Track) DownloadRequest {
	return DownloadRequest{Collection: download.Collection{Kind: kind, ID: "x", Name: "A"},
		Options: download.Options{Dir: t.TempDir(), FilenameTemplate: "{title}"}, Tracks: tracks}
}

func TestDownloadTracksValidates(t *testing.T) {
	withService := func(r DownloadRequest, service string) DownloadRequest {
		r.Tracks[0].Service = service
		return r
	}
	for _, tc := range []struct {
		name    string
		ffmpeg  string
		r       DownloadRequest
		wantErr string
	}{
		{"unknown format", "ff", req(t, "wav", "", "B00A3SGXAA"), `unknown download format "wav"`},
		{"unknown source", "ff", req(t, "", "zzz", "B00A3SGXAA"), `unknown download source "zzz"`},
		{"no ffmpeg", "", req(t, "mp3-320", "", "B00A3SGXAA"), "FFmpeg is needed"},
		{"only empty ids", "ff", req(t, "", "", ""), "a track has no id"},
		{"unknown service", "ff", withService(req(t, "", "", "B00A3SGXAA"), "tidal"), `unknown service "tidal"`},
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

	// A track whose source plugin is off would wait unplaced: refuse it with the usual "switched off".
	a, fetches := newTestApp(t, "ff")
	if err := a.SetPluginEnabled("amazon", false, false); err != nil {
		t.Fatal(err)
	}
	if err := a.DownloadTracks(req(t, "", "", "B00A3SGXAA")); err == nil || err.Error() != "Amazon Music is switched off" {
		t.Fatalf("source off: err = %v", err)
	}
	if n := len(a.GetDownloadState().Items); n != 0 {
		t.Fatalf("source off: enqueued %d", n)
	}

	a, fetches = newTestApp(t, "ff")
	if err := a.DownloadTracks(withService(req(t, "mp3-320", "lucida", "B00A3SGXAA", ""), "amazon")); err != nil { // the empty id is skipped
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
		// A track with no id is skipped, not fatal to the rest.
		{"album", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1"), sp("", "al1"), sp("4uLU6hMCjMI75M1A2tKUQC", "al1")}, []string{"album:al1", "album:al1"}},
		{"artist", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1"), sp("4uLU6hMCjMI75M1A2tKUQC", "al2")}, []string{"album:al1", "album:al2"}},
		{"playlist", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1"), sp("4uLU6hMCjMI75M1A2tKUQC", "al2")}, []string{"", ""}},
		{"track", []download.Track{sp("6rqhFgbbKwnb9MLmUQDhG6", "al1")}, []string{""}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			a, _ := newTestApp(t, "")
			if err := a.DownloadTracks(spotifyReq(t, tc.kind, tc.tracks...)); err != nil {
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
	r := req(t, "", "", "B00A3SGXAA")
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

func TestGetDownloadStateItemsNeverNil(t *testing.T) {
	a, _ := newTestApp(t, "")
	b, _ := json.Marshal(a.GetDownloadState())
	if !strings.Contains(string(b), `"items":[]`) || !strings.Contains(string(b), `"held":false`) {
		t.Fatalf("got %s", b)
	}
}

// TestFetch runs the real fetch through a queue; DownloadTracks is skipped so fetch's own FFmpeg check is hit.
func TestFetch(t *testing.T) {
	file := testFLAC(t, nil, nil)
	for _, tc := range []struct {
		name, format, ffmpeg string
		want                 download.Status
		wantErr              string
	}{
		{"original", "original", "", download.Completed, ""},
		{"no ffmpeg", "mp3-320", "", download.Failed, "FFmpeg is needed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newPluginApp(t, t.TempDir(), nil, stdPlugs(file)...)
			a.findFFmpeg = func(string) string { return tc.ffmpeg }
			r := req(t, tc.format, "auto", "B00A3SGXAA")
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
			if it.Status != tc.want || !strings.Contains(it.Error, tc.wantErr) || (tc.want == download.Completed && it.Path != filepath.Join(r.Options.Dir, "B00A3SGXAA.flac")) {
				t.Errorf("item = %+v", it)
			}
		})
	}
}

func TestFetchMarksSiteDown(t *testing.T) {
	for _, tc := range []struct {
		fail string
		is   error
		down bool
	}{{"unavailable", source.ErrUnavailable, true}, {"needs", source.ErrNeedsCheck, true}, {"boom", nil, false}} {
		a := newPluginApp(t, t.TempDir(), nil,
			srcPlug("amazon", "Amazon Music", nil, amazonCan...), dlPlug("lucida", "Lucida", map[string]any{"fail": tc.fail}))
		_, _, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "original"})
		if err == nil || errors.Is(err, download.ErrSiteDown) != tc.down || (tc.is != nil && !errors.Is(err, tc.is)) {
			t.Errorf("%s: err = %v", tc.fail, err)
		}
	}
}

func TestFetchNoFFmpegErrorHasNoSentinel(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	_, _, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "mp3-320"})
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

// guardApp has today's fake plugins, lucida's guard limit set to limit.
func guardApp(t *testing.T, dir string, limit int) *App {
	t.Helper()
	a := newPluginApp(t, dir, nil, stdPlugs(testFLAC(t, nil, nil))...)
	if err := a.SetPluginSettings("lucida", map[string]any{"$guardLimit": float64(limit)}); err != nil {
		t.Fatal(err)
	}
	return a
}

// TestDownloadGuardFlow runs the real fetch, balancer and queue: lucida asks at the limit while doubledouble does not.
func TestDownloadGuardFlow(t *testing.T) {
	enqueue := func(a *App) {
		t.Helper()
		if err := a.DownloadTracks(req(t, "original", "lucida", "B00A3SGXA1", "B00A3SGXA2", "B00A3SGXA3")); err != nil {
			t.Fatal(err)
		}
	}
	done, queued := download.Completed, download.Queued

	a := guardApp(t, t.TempDir(), 2)
	enqueue(a)
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

	a = guardApp(t, t.TempDir(), 2)
	enqueue(a)
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
	a := guardApp(t, t.TempDir(), 1)
	if err := a.DownloadTracks(req(t, "original", "lucida", "B00A3SGXA1")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return site(a, "lucida").Asking })
	if err := a.GuardWait("lucida", 45); err == nil || err.Error() != "guard wait must be 15, 30, 60 or 120 minutes" {
		t.Fatalf("GuardWait 45: err = %v", err)
	}
	if err := a.GuardWait("nope", 30); err == nil {
		t.Fatal("waited on an unknown site")
	}
	if s := site(a, "lucida"); !s.Asking || s.PausedUntil != 0 {
		t.Fatalf("a rejected wait changed lucida: %+v", s)
	}
	if err := a.GuardWait("lucida", 0); err != nil {
		t.Fatal(err)
	}
	if d := time.UnixMilli(site(a, "lucida").PausedUntil).Sub(time.Now().Add(30 * time.Minute)); d < -time.Minute || d > time.Minute {
		t.Fatalf("0 should mean the site's 30 minutes; off by %v", d)
	}
}

// A fixed source that needs the user is refused up front, as the doubledouble check was.
func TestDownloadTracksFixedSourceNeedsAction(t *testing.T) {
	a, fetches := newTestApp(t, "ff")
	if err := a.RunPluginAction("doubledouble", "lock"); err != nil {
		t.Fatal(err)
	}
	err := a.DownloadTracks(req(t, "", "doubledouble", "B00A3SGXAA"))
	if err == nil || err.Error() != "DoubleDouble: Log in" { // the site's label, as the user knows it
		t.Fatalf("err = %v", err)
	}
	if n := len(a.GetDownloadState().Items); n != 0 || len(fetches()) != 0 {
		t.Fatalf("enqueued %d, fetched %d; want none", n, len(fetches()))
	}
	if err := a.DownloadTracks(req(t, "", "lucida", "B00A3SGXAB")); err != nil {
		t.Fatalf("lucida needs nothing: %v", err)
	}
	if err := a.RunPluginAction("doubledouble", "check"); err != nil {
		t.Fatal(err)
	}
	if err := a.DownloadTracks(req(t, "", "doubledouble", "B00A3SGXAA")); err != nil {
		t.Fatalf("after the check: %v", err)
	}
}

// reviewApp has one Spotify track in review, with no candidates: the Amazon fake finds nothing.
func reviewApp(t *testing.T, dir string) *App {
	t.Helper()
	a := newPluginApp(t, dir, nil, stdPlugs(testFLAC(t, nil, nil))...)
	tr := download.Track{ID: "6rqhFgbbKwnb9MLmUQDhG6", Title: "T", Service: "spotify", AlbumID: "al1"}
	if err := a.DownloadTracks(spotifyReq(t, "album", tr)); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, a, download.Review)
	return a
}

func TestChooseCandidateCachesOnlyAfterChoose(t *testing.T) {
	const id = "6rqhFgbbKwnb9MLmUQDhG6"
	a := reviewApp(t, t.TempDir())
	if it := a.GetDownloadState().Items[0]; it.ReviewService != "amazon" {
		t.Fatalf("review service %q", it.ReviewService)
	}
	if err := a.ChooseCandidate("4uLU6hMCjMI75M1A2tKUQC", "B00A3SGXAA"); err == nil {
		t.Fatal("chose for a track that is not in review")
	}
	if _, _, _, ok := a.cache.Get("spotify", "amazon", "4uLU6hMCjMI75M1A2tKUQC"); ok {
		t.Fatal("a failed choice was cached")
	}
	if err := a.ChooseCandidate(id, ""); err == nil {
		t.Fatal("an empty choice was accepted")
	}
	if _, _, _, ok := a.cache.Get("spotify", "amazon", id); ok {
		t.Fatal("an empty choice was cached")
	}
	if err := a.ChooseCandidate(id, "B00A3SGXAA"); err != nil {
		t.Fatal(err)
	}
	if amz, via, _, _ := a.cache.Get("spotify", "amazon", id); amz != "B00A3SGXAA" || via != "manual" {
		t.Fatalf("cache = %q, %q", amz, via)
	}
	waitStatus(t, a, download.Completed)
}

func TestUseLink(t *testing.T) {
	const id = "6rqhFgbbKwnb9MLmUQDhG6"
	a := reviewApp(t, t.TempDir())
	for _, link := range []string{"https://amazon/album/B00A3SGWYC", "https://spotify/track/x", "https://example.com/x"} {
		if err := a.UseLink(id, link); err == nil || err.Error() != "paste a link to a track on Amazon Music" {
			t.Fatalf("%s: err = %v", link, err)
		}
	}
	if err := a.UseLink("4uLU6hMCjMI75M1A2tKUQC", "https://amazon/track/B00A3SGXAB"); err == nil {
		t.Fatal("used a link for a track that is not in review")
	}
	waitStatus(t, a, download.Review)
	if err := a.UseLink(id, "https://amazon/track/B00A3SGXAB"); err != nil {
		t.Fatal(err)
	}
	if amz, via, _, _ := a.cache.Get("spotify", "amazon", id); amz != "https://amazon/track/B00A3SGXAB" || via != "manual" { // the fake's id is the link
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

	b := newPluginApp(t, dir, nil, stdPlugs("")...)
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
	b := newPluginApp(t, dir, nil, stdPlugs("")...)
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
	a := newPluginApp(t, dir, nil, stdPlugs("")...)
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
	a := newPluginApp(t, dir, func(ctx context.Context, _ string, _ download.Track, _ map[string]string, _ download.Options, _ string, _ func(string)) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}, stdPlugs("")...)
	if err := a.DownloadTracks(req(t, "original", "lucida", "B00A3SGXAA")); err != nil {
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
	a := guardApp(t, dir, 1)
	if err := a.DownloadTracks(req(t, "original", "lucida", "B00A3SGXA1")); err != nil {
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

	b := newPluginApp(t, dir, nil, stdPlugs("")...)
	if got := site(b, "lucida"); got.PausedUntil != until || got.Count != 0 {
		t.Fatalf("restored lucida = %+v, want PausedUntil %d and Count 0", got, until)
	}
	if b.sources.Ready("lucida") {
		t.Fatal("lucida is ready during its guard pause")
	}
}

// TestGuardWaitTakesTheDialogsMinutes restores a queue saved with the old guard and genre keys, which are ignored now:
// the guard is the plugin's setting, and "Wait" pauses for the minutes it is given.
func TestGuardWaitTakesTheDialogsMinutes(t *testing.T) {
	dir := t.TempDir()
	out, _ := json.Marshal(t.TempDir())
	raw := `{"version":1,"items":[{"item":{"id":"B00A3SGXA1","track":{"id":"B00A3SGXA1","title":"T"},"status":"queued"},
		"collection":{"kind":"track","id":"B00A3SGXA1","name":"T"},
		"options":{"dir":` + string(out) + `,"format":"original","source":"lucida","filename_template":"{title}",
		"guard_limit":5,"guard_wait":120,"embed_genre":true,"single_genre":true}}],"pauses":{}}`
	if err := os.WriteFile(filepath.Join(dir, "queue.json"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	a := guardApp(t, dir, 1)
	if n := len(a.GetDownloadState().Items); n != 1 {
		t.Fatalf("restored %d items", n)
	}
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
	t.Setenv("LOCALAPPDATA", t.TempDir())
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

func TestExistingDir(t *testing.T) {
	dir := t.TempDir()
	if got := existingDir(filepath.Join(dir, "missing", "deeper")); got != dir {
		t.Fatalf("got %q, want %q", got, dir)
	}
	if got := existingDir(""); got != "" {
		t.Fatalf("got %q", got)
	}
}
