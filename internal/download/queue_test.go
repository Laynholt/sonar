package download

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type call struct {
	id, pathNoExt string
	o             Options
}

// fakeFetch records calls; fn decides the outcome (default: success at pathNoExt+".flac").
type fakeFetch struct {
	mu    sync.Mutex
	calls []call
	fn    func(ctx context.Context, id, pathNoExt string) (string, error)
}

func (f *fakeFetch) fetch(ctx context.Context, id string, o Options, pathNoExt string, onStatus func(string)) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{id, pathNoExt, o})
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
	return Options{Dir: t.TempDir(), Format: "original", Source: "lucida", FilenameTemplate: "{title}"}
}

var album = Collection{Kind: "album", ID: "al1", Name: "Album"}

func newQ(t *testing.T, f *fakeFetch, workers int) *Queue {
	q := NewQueue(f.fetch, workers)
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
		if c.pathNoExt != PathNoExt(tr, album, o) || c.o != o || c.o.Source != "lucida" {
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
	if find(s, "2").Error != "boom" {
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
	q := NewQueue(f.fetch, 1)
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
