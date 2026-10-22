package download

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type call struct {
	site, id, pathNoExt string
	o                   Options
}

// fakeFetch records calls; fn decides the outcome (default: success at pathNoExt+".flac").
type fakeFetch struct {
	mu    sync.Mutex
	calls []call
	fn    func(ctx context.Context, id, pathNoExt string) (string, error)
}

func (f *fakeFetch) fetch(ctx context.Context, site, id string, o Options, pathNoExt string, onStatus func(string)) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{site, id, pathNoExt, o})
	f.mu.Unlock()
	onStatus("working")
	if f.fn != nil {
		return f.fn(ctx, id, pathNoExt)
	}
	return pathNoExt + ".flac", nil
}

func (f *fakeFetch) callLog() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

func (f *fakeFetch) called(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.id == id {
			n++
		}
	}
	return n
}

func waitFor(t *testing.T, q *Queue, cond func([]Item) bool) []Item {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s := q.Snapshot()
		if cond(s) {
			return s
		}
		if time.Now().After(deadline) {
			t.Fatalf("condition not met in 2s; snapshot: %+v", s)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func allIn(want Status, ids ...string) func([]Item) bool {
	return func(s []Item) bool {
		for _, id := range ids {
			if find(s, id).Status != want {
				return false
			}
		}
		return true
	}
}

func find(s []Item, id string) Item {
	for _, it := range s {
		if it.ID == id {
			return it
		}
	}
	return Item{}
}

func tracks(ids ...string) []Track {
	var ts []Track
	for i, id := range ids {
		ts = append(ts, Track{ID: id, Title: "t" + id, Artist: "a", Position: i + 1})
	}
	return ts
}

func testOpts(t *testing.T) Options {
	return Options{Dir: t.TempDir(), Format: "original", Source: "auto", FilenameTemplate: "{title}"}
}

var album = Collection{Kind: "album", ID: "al1", Name: "Album"}

func newQ(t *testing.T, f *fakeFetch, sites int) *Queue {
	q := NewQueue(f.fetch, nil, readySites(sites))
	t.Cleanup(q.Close)
	return q
}

func TestQueueDownloadsAll(t *testing.T) {
	f := &fakeFetch{}
	q := newQ(t, f, 2)
	o := testOpts(t)
	ts := tracks("1", "2", "3")
	q.Enqueue(album, o, ts)
	s := waitFor(t, q, allIn(Completed, "1", "2", "3"))
	if len(s) != 3 {
		t.Fatalf("items = %d", len(s))
	}
	for i, it := range s {
		if !strings.HasSuffix(it.Path, ".flac") || it.QueueID != "album:al1" || it.Track.ID != ts[i].ID {
			t.Errorf("item %d = %+v", i, it)
		}
	}
	for _, c := range f.calls {
		var tr Track
		for _, x := range ts {
			if x.ID == c.id {
				tr = x
			}
		}
		if c.pathNoExt != PathNoExt(tr, album, o) || c.o != o {
			t.Errorf("call = %+v", c)
		}
	}
}

func TestQueueSkipsExistingWithoutFetching(t *testing.T) {
	f := &fakeFetch{}
	q := newQ(t, f, 1)
	o := testOpts(t)
	ts := tracks("1", "2", "3")
	want := PathNoExt(ts[1], album, o) + ".flac"
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	q.Enqueue(album, o, ts)
	s := waitFor(t, q, func(s []Item) bool {
		return find(s, "1").Status == Completed && find(s, "2").Status == Skipped && find(s, "3").Status == Completed
	})
	if find(s, "2").Path != want || f.called("2") != 0 {
		t.Errorf("item 2 = %+v, calls = %d", find(s, "2"), f.called("2"))
	}
}

func TestQueueOneFailureDoesNotStopOthers(t *testing.T) {
	f := &fakeFetch{fn: func(_ context.Context, id, p string) (string, error) {
		if id == "2" {
			return "", errors.New("boom")
		}
		return p + ".flac", nil
	}}
	q := newQ(t, f, 1)
	q.Enqueue(album, testOpts(t), tracks("1", "2", "3"))
	s := waitFor(t, q, func(s []Item) bool {
		return find(s, "1").Status == Completed && find(s, "2").Status == Failed && find(s, "3").Status == Completed
	})
	if find(s, "2").Error != "a: boom" {
		t.Errorf("error = %q", find(s, "2").Error)
	}
}

func TestQueueDedupesActiveAndRequeuesFinished(t *testing.T) {
	gate := make(chan struct{})
	f := &fakeFetch{}
	f.fn = func(_ context.Context, id, p string) (string, error) {
		if f.called(id) == 1 {
			<-gate
			return "", errors.New("boom")
		}
		return p + ".flac", nil
	}
	q := newQ(t, f, 1)
	o := testOpts(t)
	q.Enqueue(album, o, tracks("A"))
	waitFor(t, q, allIn(Downloading, "A"))
	q.Enqueue(album, o, tracks("A"))
	close(gate)
	waitFor(t, q, allIn(Failed, "A"))
	if n := f.called("A"); n != 1 {
		t.Fatalf("fetched %d times", n)
	}
	q.Enqueue(Collection{Kind: "playlist", ID: "p1", Name: "P"}, o, tracks("A"))
	s := waitFor(t, q, allIn(Completed, "A"))
	if it := find(s, "A"); len(s) != 1 || it.QueueID != "playlist:p1" || it.Error != "" {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestQueueCancelActive(t *testing.T) {
	f := &fakeFetch{fn: func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	q := newQ(t, f, 1)
	q.Enqueue(album, testOpts(t), tracks("A", "B"))
	waitFor(t, q, allIn(Downloading, "A"))
	q.Cancel("B")
	if find(q.Snapshot(), "B").Status != Cancelled {
		t.Errorf("B = %+v", find(q.Snapshot(), "B"))
	}
	q.Cancel("A")
	s := waitFor(t, q, allIn(Cancelled, "A", "B"))
	if find(s, "A").Error != "" || f.called("B") != 0 {
		t.Errorf("A = %+v, B calls = %d", find(s, "A"), f.called("B"))
	}
}

func TestQueueRespectsWorkerLimit(t *testing.T) {
	var mu sync.Mutex
	cur, peak := 0, 0
	gate := make(chan struct{})
	f := &fakeFetch{fn: func(ctx context.Context, _, p string) (string, error) {
		mu.Lock()
		cur++
		peak = max(peak, cur)
		mu.Unlock()
		defer func() {
			mu.Lock()
			cur--
			mu.Unlock()
		}()
		select {
		case <-gate:
			return p + ".flac", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	q := newQ(t, f, 2)
	q.Enqueue(album, testOpts(t), tracks("1", "2", "3", "4", "5"))
	inFlight := func() int {
		mu.Lock()
		defer mu.Unlock()
		return cur
	}
	for deadline := time.Now().Add(2 * time.Second); inFlight() != 2; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("in flight = %d, want 2", inFlight())
		}
	}
	// A third fetch would start within this window if the limit were broken.
	for end := time.Now().Add(30 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if n := len(f.callLog()); n != 2 {
			t.Fatalf("calls = %d while 2 are blocked, want 2", n)
		}
	}
	for range 5 {
		gate <- struct{}{}
	}
	waitFor(t, q, allIn(Completed, "1", "2", "3", "4", "5"))
	mu.Lock()
	defer mu.Unlock()
	if peak != 2 {
		t.Errorf("peak in flight = %d, want 2", peak)
	}
}

func TestQueueRetryAndClear(t *testing.T) {
	var mu sync.Mutex
	failing := map[string]bool{"X": true, "Y": true}
	gate := make(chan struct{})
	f := &fakeFetch{fn: func(ctx context.Context, id, p string) (string, error) {
		mu.Lock()
		fail := failing[id]
		mu.Unlock()
		switch {
		case fail:
			return "", errors.New("boom")
		case id == "W":
			select {
			case <-gate:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		return p + ".flac", nil
	}}
	q := newQ(t, f, 2)
	o := testOpts(t)
	q.Enqueue(album, o, tracks("X", "Y", "Z"))
	waitFor(t, q, func(s []Item) bool {
		return find(s, "X").Status == Failed && find(s, "Y").Status == Failed && find(s, "Z").Status == Completed
	})
	mu.Lock()
	failing["X"], failing["Y"] = false, false
	mu.Unlock()

	q.Retry("X")
	s := waitFor(t, q, allIn(Completed, "X"))
	if it := find(s, "X"); it.Error != "" || !strings.HasSuffix(it.Path, ".flac") || find(s, "Y").Status != Failed {
		t.Errorf("after Retry: %+v", s)
	}
	q.RetryFailed()
	waitFor(t, q, allIn(Completed, "X", "Y", "Z"))

	q.Enqueue(album, o, tracks("W"))
	waitFor(t, q, allIn(Downloading, "W"))
	q.ClearFinished()
	if s := q.Snapshot(); len(s) != 1 || s[0].ID != "W" {
		t.Errorf("after ClearFinished: %+v", s)
	}
	close(gate)
	waitFor(t, q, allIn(Completed, "W"))
}

func TestQueueCloseCancelsAndWaits(t *testing.T) {
	returned := make(chan struct{})
	f := &fakeFetch{fn: func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done()
		close(returned)
		return "", ctx.Err()
	}}
	q := NewQueue(f.fetch, nil, readySites(1))
	q.Enqueue(album, testOpts(t), tracks("A", "B"))
	waitFor(t, q, allIn(Downloading, "A"))
	q.Close()
	select {
	case <-returned:
	default:
		t.Fatal("Close returned before the fetch did")
	}
	s := q.Snapshot()
	if find(s, "A").Status != Cancelled || find(s, "B").Status != Cancelled {
		t.Errorf("snapshot = %+v", s)
	}
	q.Enqueue(album, testOpts(t), tracks("C"))
	if len(q.Snapshot()) != 2 {
		t.Error("Enqueue after Close added an item")
	}
}

func TestQueueSnapshotEmptyIsNotNil(t *testing.T) {
	q := newQ(t, &fakeFetch{}, 1)
	if q.Snapshot() == nil {
		t.Error("nil snapshot")
	}
}

// Enqueue releases a hold, so each hold here is set while a blocked download keeps the rest queued.
func TestQueueHoldAndResume(t *testing.T) {
	gates := map[string]chan struct{}{"A": make(chan struct{}), "E": make(chan struct{})}
	f := &fakeFetch{fn: func(_ context.Context, id, p string) (string, error) {
		if g := gates[id]; g != nil {
			<-g
		}
		return p + ".flac", nil
	}}
	q := newQ(t, f, 1)
	q.Enqueue(album, testOpts(t), tracks("A", "B", "C"))
	waitFor(t, q, allIn(Downloading, "A"))
	q.Hold(time.Time{})
	if held, until := q.HeldUntil(); !held || !until.IsZero() {
		t.Fatalf("HeldUntil = %v, %v", held, until)
	}
	close(gates["A"])
	waitFor(t, q, allIn(Completed, "A"))
	time.Sleep(100 * time.Millisecond)
	if !allIn(Queued, "B", "C")(q.Snapshot()) || len(f.callLog()) != 1 {
		t.Fatalf("held queue ran: %+v", q.Snapshot())
	}
	q.Cancel("B")
	if find(q.Snapshot(), "B").Status != Cancelled {
		t.Fatalf("B = %+v", find(q.Snapshot(), "B"))
	}
	q.Resume()
	waitFor(t, q, allIn(Completed, "C"))
	if held, _ := q.HeldUntil(); held {
		t.Fatal("still held after Resume")
	}

	q.Enqueue(album, testOpts(t), tracks("E", "F"))
	waitFor(t, q, allIn(Downloading, "E"))
	q.Hold(time.Now().Add(50 * time.Millisecond))
	close(gates["E"])
	waitFor(t, q, allIn(Completed, "E", "F"))
	if held, _ := q.HeldUntil(); held {
		t.Fatal("still held after the deadline")
	}
}

func TestQueueHoldLetsInFlightFinishAndReplacesTimer(t *testing.T) {
	release := make(chan struct{})
	f := &fakeFetch{fn: func(_ context.Context, id, p string) (string, error) {
		if id == "A" {
			<-release
		}
		return p + ".flac", nil
	}}
	q := newQ(t, f, 1)
	q.Enqueue(album, testOpts(t), tracks("A", "B"))
	waitFor(t, q, allIn(Downloading, "A"))
	q.Hold(time.Now().Add(30 * time.Millisecond))
	q.Hold(time.Time{}) // replaces the timed hold; the old timer must not lift it
	close(release)
	waitFor(t, q, allIn(Completed, "A"))
	time.Sleep(100 * time.Millisecond)
	if find(q.Snapshot(), "B").Status != Queued {
		t.Fatalf("B = %+v; the stale timer resumed the queue", find(q.Snapshot(), "B"))
	}
}

func TestQueueCloseWhileHeld(t *testing.T) {
	q := NewQueue((&fakeFetch{}).fetch, nil, readySites(2))
	q.Enqueue(album, testOpts(t), tracks("A"))
	q.Hold(time.Time{}) // after Enqueue, which releases a hold
	done := make(chan struct{})
	go func() { q.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close hangs while held")
	}
}

func TestQueueSameTargetWaitsForRunningDownload(t *testing.T) {
	gate := make(chan struct{})
	f := &fakeFetch{fn: func(ctx context.Context, _, p string) (string, error) {
		select {
		case <-gate:
			return p + ".flac", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	q := newQ(t, f, 2)
	ts := tracks("1", "2")
	ts[1].Title = ts[0].Title // an album and its deluxe edition share a song
	q.Enqueue(album, testOpts(t), ts)
	waitFor(t, q, func(s []Item) bool { return find(s, "1").Status == Downloading })
	// The second worker would start track 2 within this window if the paths were not compared.
	for end := time.Now().Add(30 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if n := len(f.callLog()); n != 1 {
			t.Fatalf("calls = %d while the same target is downloading, want 1", n)
		}
	}
	gate <- struct{}{}
	gate <- struct{}{}
	waitFor(t, q, allIn(Completed, "1", "2"))
}

func TestQueueReportsProgress(t *testing.T) {
	release := make(chan struct{})
	q := NewQueue(func(ctx context.Context, _, _ string, _ Options, pathNoExt string, _ func(string)) (string, error) {
		Report(ctx, 5, 10)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return pathNoExt + ".flac", nil
	}, nil, readySites(1))
	t.Cleanup(q.Close)
	q.Enqueue(album, testOpts(t), tracks("1"))
	waitFor(t, q, func(s []Item) bool { it := find(s, "1"); return it.Bytes == 5 && it.Size == 10 })
	close(release)
	waitFor(t, q, allIn(Completed, "1"))
}

// fakeResolve records the track IDs of each call and the peak of overlapping calls.
// fn decides the outcome (default: a confident match "B-"+ID via "title").
type fakeResolve struct {
	mu        sync.Mutex
	calls     [][]string
	cur, peak int
	fn        func(ctx context.Context, ts []Track) []Resolution
}

func (r *fakeResolve) resolve(ctx context.Context, ts []Track) []Resolution {
	ids := make([]string, len(ts))
	for i, t := range ts {
		ids[i] = t.ID
	}
	r.mu.Lock()
	r.calls = append(r.calls, ids)
	r.cur++
	r.peak = max(r.peak, r.cur)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.cur--
		r.mu.Unlock()
	}()
	if r.fn != nil {
		return r.fn(ctx, ts)
	}
	out := make([]Resolution, len(ts))
	for i, t := range ts {
		out[i] = Resolution{AmazonID: "B-" + t.ID, Via: "title"}
	}
	return out
}

func (r *fakeResolve) callLog() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

func newMQ(t *testing.T, f *fakeFetch, r *fakeResolve) *Queue {
	q := NewQueue(f.fetch, r.resolve, readySites(2))
	t.Cleanup(q.Close)
	return q
}

func spotify(ids ...string) []Track {
	ts := tracks(ids...)
	for i := range ts {
		ts[i].Service = "spotify"
	}
	return ts
}

func each(ts []Track, fn func(Track) Resolution) []Resolution {
	out := make([]Resolution, len(ts))
	for i, t := range ts {
		out[i] = fn(t)
	}
	return out
}

// wait blocks until gate yields or the queue closes, so a stuck fake never outlives its test.
func wait(ctx context.Context, gate chan struct{}) {
	select {
	case <-gate:
	case <-ctx.Done():
	}
}

var twoCandidates = []Candidate{{ID: "B000000002", Score: 0.7, Recommended: true}, {ID: "B000000003", Score: 0.6}}

func reviewAll(ctx context.Context, ts []Track) []Resolution {
	return each(ts, func(Track) Resolution { return Resolution{Candidates: twoCandidates} })
}

func TestSpotifyTrackMatchedThenDownloaded(t *testing.T) {
	f := &fakeFetch{}
	r := &fakeResolve{fn: func(_ context.Context, ts []Track) []Resolution {
		return each(ts, func(Track) Resolution { return Resolution{AmazonID: "B000000001", Via: "isrc"} })
	}}
	q := newMQ(t, f, r)
	q.Enqueue(album, testOpts(t), spotify("s1"))
	it := find(waitFor(t, q, allIn(Completed, "s1")), "s1")
	if it.AmazonID != "B000000001" || it.Via != "isrc" {
		t.Errorf("item = %+v", it)
	}
	if c := f.callLog(); len(c) != 1 || c[0].id != "B000000001" {
		t.Errorf("fetch calls = %+v", c)
	}
}

func TestUncertainGoesToReview(t *testing.T) {
	f := &fakeFetch{}
	q := newMQ(t, f, &fakeResolve{fn: reviewAll})
	q.Enqueue(album, testOpts(t), spotify("s1"))
	it := find(waitFor(t, q, allIn(Review, "s1")), "s1")
	if len(it.Candidates) != 2 || it.AmazonID != "" {
		t.Errorf("item = %+v", it)
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(f.callLog()); n != 0 {
		t.Errorf("fetch called %d times for a track in review", n)
	}
}

func TestChooseQueuesCandidate(t *testing.T) {
	f := &fakeFetch{}
	q := newMQ(t, f, &fakeResolve{fn: reviewAll})
	q.Enqueue(album, testOpts(t), spotify("s1"))
	waitFor(t, q, allIn(Review, "s1"))
	if err := q.Choose("s1", "B000000002"); err != nil {
		t.Fatal(err)
	}
	it := find(waitFor(t, q, allIn(Completed, "s1")), "s1")
	if it.AmazonID != "B000000002" || it.Via != "manual" {
		t.Errorf("item = %+v", it)
	}
	if c := f.callLog(); len(c) != 1 || c[0].id != "B000000002" {
		t.Errorf("fetch calls = %+v", c)
	}
	if err := q.Choose("s1", "B000000003"); err == nil {
		t.Error("Choose on a completed item succeeded")
	}
	if err := q.Choose("nope", "B000000003"); err == nil {
		t.Error("Choose on an unknown item succeeded")
	}
}

func TestSkipNotFoundAndIgnored(t *testing.T) {
	q := newMQ(t, &fakeFetch{}, &fakeResolve{fn: func(_ context.Context, ts []Track) []Resolution {
		return each(ts, func(t Track) Resolution {
			if t.ID == "none" {
				return Resolution{}
			}
			return Resolution{Candidates: twoCandidates}
		})
	}})
	q.Enqueue(album, testOpts(t), spotify("none", "some"))
	waitFor(t, q, allIn(Review, "none", "some"))
	q.Skip("none")
	q.Skip("some")
	s := q.Snapshot()
	if find(s, "none").Status != NotFound || find(s, "some").Status != Ignored {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestGroupResolvedTogether(t *testing.T) {
	r := &fakeResolve{}
	r.fn = func(_ context.Context, ts []Track) []Resolution {
		time.Sleep(10 * time.Millisecond) // long enough for an overlapping call to show in peak
		return each(ts, func(t Track) Resolution { return Resolution{AmazonID: "B-" + t.ID} })
	}
	q := newMQ(t, &fakeFetch{}, r)
	ts := spotify("g1", "s4", "g2", "g3")
	for _, i := range []int{0, 2, 3} {
		ts[i].Group = "album:X"
	}
	q.Enqueue(album, testOpts(t), ts)
	waitFor(t, q, allIn(Completed, "g1", "s4", "g2", "g3"))
	if c := fmt.Sprint(r.callLog()); c != "[[g1 g2 g3] [s4]]" {
		t.Errorf("resolve calls = %s", c)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.peak != 1 {
		t.Errorf("peak concurrent resolves = %d, want 1", r.peak)
	}
}

func TestPlaylistMatchedOneByOne(t *testing.T) {
	gate, release := make(chan struct{}), make(chan struct{})
	r := &fakeResolve{}
	r.fn = func(ctx context.Context, ts []Track) []Resolution {
		wait(ctx, gate)
		return each(ts, func(t Track) Resolution { return Resolution{AmazonID: "B-" + t.ID} })
	}
	f := &fakeFetch{fn: func(ctx context.Context, _, p string) (string, error) {
		wait(ctx, release)
		return p + ".flac", nil
	}}
	q := newMQ(t, f, r)
	q.Enqueue(Collection{Kind: "playlist", ID: "p1", Name: "P"}, testOpts(t), spotify("1", "2", "3"))
	gate <- struct{}{}
	waitFor(t, q, func(s []Item) bool { return find(s, "1").Status == Downloading && find(s, "3").Status == Matching })
	close(gate)
	close(release)
	waitFor(t, q, allIn(Completed, "1", "2", "3"))
	if c := fmt.Sprint(r.callLog()); c != "[[1] [2] [3]]" {
		t.Errorf("resolve calls = %s", c)
	}
}

func TestMatchErrorRetryRematches(t *testing.T) {
	gate := make(chan struct{})
	r := &fakeResolve{}
	r.fn = func(ctx context.Context, ts []Track) []Resolution {
		if len(r.callLog()) == 1 {
			return each(ts, func(Track) Resolution { return Resolution{Err: errors.New("no network")} })
		}
		wait(ctx, gate)
		return each(ts, func(t Track) Resolution { return Resolution{AmazonID: "B-" + t.ID} })
	}
	q := newMQ(t, &fakeFetch{}, r)
	q.Enqueue(album, testOpts(t), spotify("s1"))
	if it := find(waitFor(t, q, allIn(Failed, "s1")), "s1"); it.Error != "no network" {
		t.Errorf("item = %+v", it)
	}
	q.Retry("s1")
	if it := find(q.Snapshot(), "s1"); it.Status != Matching || it.Error != "" {
		t.Errorf("after Retry: %+v", it)
	}
	close(gate)
	if it := find(waitFor(t, q, allIn(Completed, "s1")), "s1"); it.AmazonID != "B-s1" {
		t.Errorf("item = %+v", it)
	}
}

func TestEnqueueKeepsMatchingAndReview(t *testing.T) {
	r := &fakeResolve{}
	r.fn = func(ctx context.Context, ts []Track) []Resolution {
		if ts[0].ID == "m" {
			wait(ctx, make(chan struct{})) // stays matching until Close
		}
		return reviewAll(ctx, ts)
	}
	q := newMQ(t, &fakeFetch{}, r)
	o := testOpts(t)
	q.Enqueue(album, o, spotify("r"))
	waitFor(t, q, allIn(Review, "r"))
	q.Enqueue(album, o, spotify("m"))
	waitFor(t, q, func([]Item) bool { return len(r.callLog()) == 2 })
	q.Enqueue(Collection{Kind: "playlist", ID: "p1", Name: "P"}, o, spotify("r", "m"))
	q.ClearFinished()
	s := q.Snapshot()
	if len(s) != 2 || find(s, "r").Status != Review || find(s, "m").Status != Matching ||
		find(s, "r").QueueID != "album:al1" || len(find(s, "r").Candidates) != 2 {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestAmazonTrackSkipsMatching(t *testing.T) {
	f, r := &fakeFetch{}, &fakeResolve{}
	q := newMQ(t, f, r)
	ts := tracks("A", "B")
	ts[1].Service = "amazon"
	q.Enqueue(album, testOpts(t), ts)
	s := waitFor(t, q, allIn(Completed, "A", "B"))
	if find(s, "A").AmazonID != "A" || find(s, "B").AmazonID != "B" || len(r.callLog()) != 0 {
		t.Errorf("snapshot = %+v, resolve calls = %v", s, r.callLog())
	}
	if f.called("A") != 1 || f.called("B") != 1 {
		t.Errorf("fetch calls = %+v", f.callLog())
	}
}

func TestEnqueueReleasesHold(t *testing.T) {
	q := newMQ(t, &fakeFetch{}, &fakeResolve{})
	q.Hold(time.Time{})
	q.Enqueue(album, testOpts(t), append(tracks("A"), spotify("S")...))
	if it := find(waitFor(t, q, allIn(Completed, "A", "S")), "S"); it.AmazonID != "B-S" {
		t.Errorf("S = %+v", it)
	}
	if held, _ := q.HeldUntil(); held {
		t.Error("still held after Enqueue")
	}
}

func TestNilResolveFailsSpotifyTracks(t *testing.T) {
	q := newQ(t, &fakeFetch{}, 1)
	q.Enqueue(album, testOpts(t), spotify("S"))
	if it := find(waitFor(t, q, allIn(Failed, "S")), "S"); it.Error != "no match result" {
		t.Errorf("S = %+v", it)
	}
}

func TestHoldStopsMatching(t *testing.T) {
	gate := make(chan struct{})
	f, r := &fakeFetch{}, &fakeResolve{}
	r.fn = func(ctx context.Context, ts []Track) []Resolution {
		if ts[0].ID == "A" {
			wait(ctx, gate)
		}
		return each(ts, func(t Track) Resolution { return Resolution{AmazonID: "B-" + t.ID} })
	}
	q := newMQ(t, f, r)
	q.Enqueue(album, testOpts(t), spotify("A", "B"))
	waitFor(t, q, func([]Item) bool { return len(r.callLog()) == 1 })
	q.Hold(time.Time{})
	close(gate) // the running match finishes; nothing new starts
	waitFor(t, q, allIn(Queued, "A"))
	time.Sleep(100 * time.Millisecond)
	if s := q.Snapshot(); find(s, "B").Status != Matching || len(r.callLog()) != 1 || len(f.callLog()) != 0 {
		t.Fatalf("held queue ran: %+v, resolve calls = %v", s, r.callLog())
	}
	q.Resume()
	waitFor(t, q, allIn(Completed, "A", "B"))
}

func TestCancelMatchingAndReview(t *testing.T) {
	gate := make(chan struct{})
	f, r := &fakeFetch{}, &fakeResolve{}
	r.fn = func(ctx context.Context, ts []Track) []Resolution {
		switch ts[0].ID {
		case "r":
			return reviewAll(ctx, ts)
		case "m":
			wait(ctx, gate)
		}
		return each(ts, func(t Track) Resolution { return Resolution{AmazonID: "B-" + t.ID} })
	}
	q := newMQ(t, f, r)
	o := testOpts(t)
	q.Enqueue(album, o, spotify("r"))
	waitFor(t, q, allIn(Review, "r"))
	q.Enqueue(album, o, spotify("m", "n"))
	waitFor(t, q, func([]Item) bool { return len(r.callLog()) == 2 })
	q.Cancel("r")
	q.Cancel("m")
	if !allIn(Cancelled, "r", "m")(q.Snapshot()) {
		t.Fatalf("snapshot = %+v", q.Snapshot())
	}
	close(gate)
	// Matching is serial, so once n is done the resolution for m has been applied or dropped.
	s := waitFor(t, q, allIn(Completed, "n"))
	if find(s, "m").Status != Cancelled || find(s, "m").AmazonID != "" || f.called("B-m") != 0 {
		t.Errorf("snapshot = %+v", s)
	}
}

func TestStaleResolutionDropped(t *testing.T) {
	gate := make(chan struct{})
	f, r := &fakeFetch{}, &fakeResolve{}
	r.fn = func(ctx context.Context, ts []Track) []Resolution {
		id := "B-new"
		if len(r.callLog()) == 1 {
			wait(ctx, gate)
			id = "B-old"
		}
		return each(ts, func(Track) Resolution { return Resolution{AmazonID: id} })
	}
	q := newMQ(t, f, r)
	q.Enqueue(album, testOpts(t), spotify("s1"))
	waitFor(t, q, func([]Item) bool { return len(r.callLog()) == 1 })
	q.Cancel("s1")
	q.Retry("s1")
	close(gate) // the first attempt's result arrives while the entry is matching again
	it := find(waitFor(t, q, allIn(Completed, "s1")), "s1")
	if it.AmazonID != "B-new" || len(r.callLog()) != 2 || f.called("B-old") != 0 {
		t.Errorf("item = %+v, resolve calls = %v, fetch calls = %+v", it, r.callLog(), f.callLog())
	}
}

// fakeSites has fixed names and settable readiness.
type fakeSites struct {
	names []string
	mu    sync.Mutex
	ready map[string]bool
	asked int // Ready calls
}

func newSites(names ...string) *fakeSites {
	s := &fakeSites{names: names, ready: map[string]bool{}}
	for _, n := range names {
		s.ready[n] = true
	}
	return s
}

// readySites returns n ready sites named "a", "b", ….
func readySites(n int) *fakeSites {
	var names []string
	for i := range n {
		names = append(names, string(rune('a'+i)))
	}
	return newSites(names...)
}

func (s *fakeSites) Names() []string { return s.names }

func (s *fakeSites) Ready(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked++
	return s.ready[name]
}

func (s *fakeSites) set(name string, ready bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready[name] = ready
}

// siteLog records which site each fetch ran on.
type siteLog struct {
	mu    sync.Mutex
	sites []string
}

func (l *siteLog) add(site string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sites = append(l.sites, site)
}

func (l *siteLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.sites, " ")
}

// siteQ runs fn as the fetch: nil saves pathNoExt+".flac", an error fails the attempt.
func siteQ(t *testing.T, s Sites, log *siteLog, fn func(ctx context.Context, site, id string) error) *Queue {
	q := NewQueue(func(ctx context.Context, site, id string, _ Options, p string, _ func(string)) (string, error) {
		log.add(site)
		if err := fn(ctx, site, id); err != nil {
			return "", err
		}
		return p + ".flac", nil
	}, nil, s)
	t.Cleanup(q.Close)
	return q
}

func siteDown(site string) error { return fmt.Errorf("%w: %s: HTTP 503", ErrSiteDown, site) }

func TestOneTrackPerSite(t *testing.T) {
	q := siteQ(t, newSites("a", "b"), &siteLog{}, func(ctx context.Context, _, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	})
	q.Enqueue(album, testOpts(t), tracks("1", "2", "3", "4"))
	downloading := func(s []Item) (sites []string) {
		for _, it := range s {
			if it.Status == Downloading {
				sites = append(sites, it.Site)
			}
		}
		return sites
	}
	waitFor(t, q, func(s []Item) bool { return len(downloading(s)) == 2 })
	time.Sleep(30 * time.Millisecond) // a third would start in this window if a site took two
	got := downloading(q.Snapshot())
	if len(got) != 2 || got[0] == got[1] {
		t.Fatalf("downloading on %v, want one each on a and b", got)
	}
}

func TestOneSiteReadySequential(t *testing.T) {
	s := newSites("a", "b")
	s.set("b", false)
	var cur, peak atomic.Int32
	q := siteQ(t, s, &siteLog{}, func(context.Context, string, string) error {
		n := cur.Add(1)
		defer cur.Add(-1)
		if n > peak.Load() {
			peak.Store(n)
		}
		time.Sleep(5 * time.Millisecond)
		return nil
	})
	q.Enqueue(album, testOpts(t), tracks("1", "2", "3"))
	for _, it := range waitFor(t, q, allIn(Completed, "1", "2", "3")) {
		if it.Site != "a" {
			t.Errorf("%s ran on %q", it.ID, it.Site)
		}
	}
	if peak.Load() != 1 {
		t.Errorf("peak in flight = %d, want 1", peak.Load())
	}
}

func TestNotReadySiteWaitsThenWake(t *testing.T) {
	s := newSites("a", "b")
	s.set("a", false)
	s.set("b", false)
	log := &siteLog{}
	q := siteQ(t, s, log, func(context.Context, string, string) error { return nil })
	q.Enqueue(album, testOpts(t), tracks("1"))
	time.Sleep(30 * time.Millisecond)
	if it := find(q.Snapshot(), "1"); it.Status != Queued || log.String() != "" {
		t.Fatalf("item = %+v, fetches on %q; want queued, none", it, log)
	}
	s.mu.Lock()
	asked := s.asked
	s.mu.Unlock()
	if asked > 10 { // a few per broadcast; a spinning worker asks thousands of times
		t.Fatalf("Ready called %d times while idle", asked)
	}
	s.set("a", true)
	q.Wake()
	if it := find(waitFor(t, q, allIn(Completed, "1")), "1"); it.Site != "a" {
		t.Errorf("item = %+v", it)
	}
}

func TestFixedSourceWaitsForItsSite(t *testing.T) {
	s := newSites("a", "b")
	s.set("b", false)
	log := &siteLog{}
	q := siteQ(t, s, log, func(context.Context, string, string) error { return nil })
	o := testOpts(t)
	o.Source = "b"
	q.Enqueue(album, o, tracks("1"))
	time.Sleep(30 * time.Millisecond)
	if it := find(q.Snapshot(), "1"); it.Status != Queued || log.String() != "" {
		t.Fatalf("item = %+v, fetches on %q; want queued, none", it, log)
	}
	s.set("b", true)
	q.Wake()
	if it := find(waitFor(t, q, allIn(Completed, "1")), "1"); it.Site != "b" || log.String() != "b" {
		t.Errorf("item = %+v, fetches on %q", it, log)
	}
}

// a goes down, b fails the track, a is back: a site-down never counts as tried, so a takes it again.
func TestSiteDownRequeuesInAuto(t *testing.T) {
	s := newSites("a", "b")
	s.set("b", false) // so a takes it first
	log := &siteLog{}
	aCalls := 0
	q := siteQ(t, s, log, func(_ context.Context, site, _ string) error {
		switch {
		case site == "a" && aCalls == 0:
			aCalls++
			s.set("a", false)
			s.set("b", true)
			return siteDown("a")
		case site == "b":
			s.set("a", true)
			return errors.New("boom")
		}
		return nil
	})
	q.Enqueue(album, testOpts(t), tracks("1"))
	it := find(waitFor(t, q, allIn(Completed, "1")), "1")
	if it.Site != "a" || it.Error != "" || log.String() != "a b a" {
		t.Errorf("item = %+v, fetches on %q", it, log)
	}
}

func TestSiteDownFailsInFixed(t *testing.T) {
	log := &siteLog{}
	q := siteQ(t, newSites("a", "b"), log, func(_ context.Context, site, _ string) error { return siteDown(site) })
	o := testOpts(t)
	o.Source = "a"
	q.Enqueue(album, o, tracks("1"))
	it := find(waitFor(t, q, allIn(Failed, "1")), "1")
	if !strings.Contains(it.Error, ErrSiteDown.Error()) || it.Site != "a" || log.String() != "a" {
		t.Errorf("item = %+v, fetches on %q", it, log)
	}
}

func TestTrackErrorTriesOtherSiteThenFails(t *testing.T) {
	s := newSites("a", "b")
	s.set("b", false) // so a goes first
	log := &siteLog{}
	var ok atomic.Bool
	q := siteQ(t, s, log, func(_ context.Context, site, _ string) error {
		s.set("b", true)
		if ok.Load() {
			return nil
		}
		return errors.New("no stream on " + site)
	})
	q.Enqueue(album, testOpts(t), tracks("1"))
	it := find(waitFor(t, q, allIn(Failed, "1")), "1")
	if it.Error != "a: no stream on a; b: no stream on b" || log.String() != "a b" {
		t.Errorf("item = %+v, fetches on %q", it, log)
	}

	// Without clearing "tried" both sites would pass it over and it would stay queued.
	ok.Store(true)
	q.Retry("1")
	if it := find(waitFor(t, q, allIn(Completed, "1")), "1"); it.Error != "" {
		t.Errorf("after Retry: %+v", it)
	}
}

func TestRetryClearsTried(t *testing.T) {
	var ok atomic.Bool
	q := siteQ(t, newSites("a"), &siteLog{}, func(context.Context, string, string) error {
		if ok.Load() {
			return nil
		}
		return errors.New("boom")
	})
	q.Enqueue(album, testOpts(t), tracks("1", "2"))
	waitFor(t, q, allIn(Failed, "1", "2"))
	ok.Store(true)
	q.Retry("1")
	waitFor(t, q, allIn(Completed, "1"))
	q.Enqueue(album, testOpts(t), tracks("2")) // a re-Enqueue clears them too
	waitFor(t, q, allIn(Completed, "2"))
}

func TestErrorKeepsSitePrefixOnce(t *testing.T) {
	q := siteQ(t, newSites("a"), &siteLog{}, func(context.Context, string, string) error { return errors.New("a: HTTP 404") })
	q.Enqueue(album, testOpts(t), tracks("1"))
	if it := find(waitFor(t, q, allIn(Failed, "1")), "1"); it.Error != "a: HTTP 404" {
		t.Errorf("error = %q", it.Error)
	}
}
