package download

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var playlist = Collection{Kind: "playlist", ID: "pl1", Name: "Mix"}

func TestPendingSavesDownloadingAsQueued(t *testing.T) {
	f := &fakeFetch{fn: func(ctx context.Context, id, p string) (string, error) {
		if id == "D" {
			Report(ctx, 5, 10)
			<-ctx.Done()
			return "", ctx.Err()
		}
		return p + ".flac", nil
	}}
	r := &fakeResolve{fn: func(ctx context.Context, ts []Track) []Resolution {
		if ts[0].ID == "f" {
			return []Resolution{{Err: context.DeadlineExceeded}}
		}
		return reviewAll(ctx, ts)
	}}
	q := newMQ(t, f, r)
	o := testOpts(t)
	q.Enqueue(playlist, o, tracks("D"))
	waitFor(t, q, func(s []Item) bool { return find(s, "D").Bytes == 5 })
	q.Enqueue(playlist, o, append(tracks("C"), spotify("r", "f")...))
	waitFor(t, q, func(s []Item) bool {
		return allIn(Completed, "C")(s) && allIn(Review, "r")(s) && allIn(Failed, "f")(s)
	})

	p := q.Pending()
	got := map[string]Saved{}
	for _, s := range p {
		got[s.Item.ID] = s
	}
	if len(p) != 3 || got["C"].Item.ID != "" {
		t.Fatalf("pending = %+v", p)
	}
	d := got["D"]
	if d.Item.Status != Queued || len(d.Item.IDs) != 0 || d.Item.Bytes != 0 || d.Item.Size != 0 ||
		d.Item.Message != "" || d.Item.Site != "" || d.Item.Error != "" || d.Collection != playlist || d.Options != o {
		t.Errorf("D = %+v", d)
	}
	if rv := got["r"].Item; rv.Status != Review || len(rv.Candidates) != 2 {
		t.Errorf("r = %+v", rv)
	}
	if fl := got["f"].Item; fl.Status != Failed || fl.Error == "" {
		t.Errorf("f = %+v", fl)
	}
}

func saved(o Options, id string, st Status, amazonID string) Saved {
	tr := Track{ID: id, Title: "t" + id, Artist: "a", Service: "spotify"}
	sv := Saved{Item: Item{ID: id, QueueID: "playlist:pl1", Track: tr, Status: st}, Collection: playlist, Options: o}
	if amazonID != "" {
		sv.Item.IDs = map[string]string{"amazon": amazonID}
	}
	return sv
}

func TestRestoreHoldsAndKeepsStatuses(t *testing.T) {
	f, r := &fakeFetch{}, &fakeResolve{}
	q := newMQ(t, f, r)
	o := testOpts(t)
	rv := saved(o, "r", Review, "")
	rv.Item.Candidates = twoCandidates
	fl := saved(o, "f", Failed, "")
	fl.Item.Error = "boom"
	all := []Saved{saved(o, "m", Matching, ""), saved(o, "q", Queued, "B-q"), rv, fl}
	q.Restore(all)
	if !q.Held() {
		t.Fatal("restored queue not held")
	}
	time.Sleep(50 * time.Millisecond)
	s := q.Snapshot()
	if !allIn(Matching, "m")(s) || !allIn(Queued, "q")(s) || !allIn(Review, "r")(s) || !allIn(Failed, "f")(s) ||
		len(find(s, "r").Candidates) != 2 || find(s, "f").Error != "boom" {
		t.Fatalf("snapshot = %+v", s)
	}
	if len(r.callLog()) != 0 || len(f.callLog()) != 0 {
		t.Fatalf("held queue ran: resolve %v, fetch %+v", r.callLog(), f.callLog())
	}

	q.Resume()
	waitFor(t, q, allIn(Completed, "m", "q"))
	for _, c := range f.callLog() {
		if c.pathNoExt != PathNoExt(Track{ID: "x", Title: "t" + strings.TrimPrefix(c.id, "B-"), Artist: "a"}, playlist, o) || c.o != o {
			t.Errorf("fetch call = %+v", c)
		}
	}
	q.Restore(all) // every ID is known: nothing added, no hold
	if q.Held() || len(q.Snapshot()) != 4 {
		t.Errorf("held = %v, snapshot = %+v", q.Held(), q.Snapshot())
	}
}

// Failed entries alone leave nothing to resume, so no dialog would show and nothing may hold the queue.
func TestRestoreOnlyFailedDoesNotHold(t *testing.T) {
	q := newMQ(t, &fakeFetch{}, &fakeResolve{})
	o := testOpts(t)
	q.Restore([]Saved{saved(o, "f", Failed, "")})
	if q.Held() || !allIn(Failed, "f")(q.Snapshot()) {
		t.Fatalf("held = %v, snapshot = %+v", q.Held(), q.Snapshot())
	}
	q.Restore([]Saved{saved(o, "g", Failed, ""), saved(o, "r", Review, "")})
	if !q.Held() {
		t.Fatal("a restored review entry did not hold the queue")
	}
}

func TestRestoreThenEnqueueResumes(t *testing.T) {
	q := newMQ(t, &fakeFetch{}, &fakeResolve{})
	o := testOpts(t)
	q.Restore([]Saved{saved(o, "q", Queued, "B-q")})
	q.Enqueue(album, o, tracks("n"))
	waitFor(t, q, allIn(Completed, "q", "n"))
	if q.Held() {
		t.Error("still held after Enqueue")
	}
}

func TestClearKeepsCompleted(t *testing.T) {
	var dCalls atomic.Int32
	f := &fakeFetch{fn: func(ctx context.Context, id, p string) (string, error) {
		if id == "D" && dCalls.Add(1) == 1 {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return p + ".flac", nil
	}}
	gate := make(chan struct{})
	r := &fakeResolve{fn: func(ctx context.Context, ts []Track) []Resolution {
		switch ts[0].ID {
		case "r":
			return reviewAll(ctx, ts)
		case "f":
			return []Resolution{{Err: context.DeadlineExceeded}}
		case "m":
			wait(ctx, gate)
		}
		return each(ts, func(t Track) Resolution { return Resolution{ID: "B-" + t.ID} })
	}}
	q := newMQ(t, f, r)
	o := testOpts(t)
	q.Enqueue(album, o, tracks("C", "D"))
	q.Enqueue(album, o, spotify("r", "f", "m"))
	waitFor(t, q, func(s []Item) bool {
		return allIn(Completed, "C")(s) && allIn(Downloading, "D")(s) && allIn(Review, "r")(s) && allIn(Failed, "f")(s) && len(r.callLog()) == 3
	})
	q.Hold(time.Time{})
	q.Clear()
	if s := q.Snapshot(); len(s) != 1 || s[0].ID != "C" || !q.Held() {
		t.Fatalf("held = %v, snapshot = %+v", q.Held(), s)
	}
	close(gate) // m's result arrives after Clear and is dropped
	q.Enqueue(album, o, append(tracks("D"), spotify("n")...))
	s := waitFor(t, q, allIn(Completed, "C", "D", "n"))
	if len(s) != 3 || f.called("B-m") != 0 {
		t.Errorf("snapshot = %+v, fetch calls = %+v", s, f.callLog())
	}
}

func TestClearedDownloadKeepsItsPathBusy(t *testing.T) {
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock() // a failing test must not leave Close waiting on the fetch
	var calls atomic.Int32
	f := &fakeFetch{fn: func(ctx context.Context, _, p string) (string, error) {
		if calls.Add(1) == 1 {
			<-release // a fetch slow to notice its cancel, still writing the .part
			return "", context.Canceled
		}
		return p + ".flac", nil
	}}
	q := newQ(t, f, 2)
	o := testOpts(t)
	q.Enqueue(album, o, tracks("D"))
	waitFor(t, q, allIn(Downloading, "D"))
	q.Clear()
	q.Enqueue(album, o, tracks("D"))
	// The other site is free, so it would start D within this window if the cleared download's path were not busy.
	for end := time.Now().Add(30 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
		if n := len(f.callLog()); n != 1 {
			t.Fatalf("calls = %d while the cleared download still runs, want 1", n)
		}
	}
	unblock()
	waitFor(t, q, allIn(Completed, "D"))
	if n := len(f.callLog()); n != 2 {
		t.Errorf("calls = %d, want 2", n)
	}
}

func TestOnChangeNotCalledForProgress(t *testing.T) {
	gate := make(chan struct{})
	f := &fakeFetch{fn: func(ctx context.Context, _, p string) (string, error) {
		for i := range 100 {
			Report(ctx, int64(i+1), 100)
		}
		wait(ctx, gate)
		return p + ".flac", nil
	}}
	q := newQ(t, f, 1)
	var n atomic.Int32
	q.OnChange = func() {
		q.Pending() // the saver's call: deadlocks if OnChange runs under the lock
		n.Add(1)
	}
	q.Enqueue(album, testOpts(t), tracks("A"))
	waitFor(t, q, func(s []Item) bool { return find(s, "A").Bytes == 100 })
	if got := n.Load(); got != 2 { // Enqueue, then queued -> downloading
		t.Fatalf("OnChange calls before completion = %d, want 2", got)
	}
	close(gate)
	waitFor(t, q, allIn(Completed, "A"))
	waitFor(t, q, func([]Item) bool { return n.Load() == 3 })
}

func TestOnChangeSilentAfterClose(t *testing.T) {
	f := &fakeFetch{fn: func(ctx context.Context, _, _ string) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}
	q := NewQueue(f.fetch, nil, readySites(1))
	var n atomic.Int32
	q.OnChange = func() { n.Add(1) }
	q.Enqueue(album, testOpts(t), tracks("A", "B"))
	waitFor(t, q, allIn(Downloading, "A"))
	waitFor(t, q, func([]Item) bool { return n.Load() >= 2 }) // Enqueue, then the worker's notify after taking A
	before := n.Load()
	q.Close() // its cancellations must not overwrite the save made before it
	if got := n.Load(); got != before {
		t.Errorf("OnChange calls = %d after Close, want %d", got, before)
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	o := Options{Dir: "D:/Music", Format: "flac", Source: "auto"}
	rv := saved(o, "r", Review, "")
	rv.Item.Candidates = twoCandidates
	want := State{Version: 1, Items: []Saved{saved(o, "q", Queued, "B-q"), rv}, Pauses: map[string]int64{"lucida": 1761130000000}}
	if err := SaveState(path, want); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(path, want); err != nil { // overwrites
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v; want %+v", got, err, want)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "\n  \"items\"") {
		t.Errorf("not indented:\n%s", raw)
	}
	if ents, _ := os.ReadDir(filepath.Dir(path)); len(ents) != 1 {
		t.Errorf("dir has %d files, want only queue.json", len(ents))
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	s, err := LoadState(filepath.Join(t.TempDir(), "queue.json"))
	if err != nil || !reflect.DeepEqual(s, State{Version: 1}) {
		t.Errorf("got %+v, %v", s, err)
	}
}

func TestLoadStateCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	os.WriteFile(path, []byte("{\"version\": 1, \"items\": ["), 0o644)
	if _, err := LoadState(path); err == nil {
		t.Error("corrupt file loaded without error")
	}
}

func TestPendingEmptyIsNotNil(t *testing.T) {
	q := newQ(t, &fakeFetch{}, 1)
	path := filepath.Join(t.TempDir(), "queue.json")
	if err := SaveState(path, State{Version: 1, Items: q.Pending()}); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), `"items": []`) {
		t.Errorf("file:\n%s", raw)
	}
}

// A queue.json written before per-service IDs has "amazon_id"; a review in it was always for Amazon.
func TestRestoreLegacyAmazonID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.json")
	o := `{"dir": "` + filepath.ToSlash(t.TempDir()) + `", "format": "original", "source": "auto", "filename_template": "{title}"}`
	raw := `{"version": 1, "items": [
		{"item": {"id": "s1", "track": {"id": "s1", "title": "t1", "service": "spotify"}, "status": "queued", "amazon_id": "B0X"}, "options": ` + o + `},
		{"item": {"id": "s2", "track": {"id": "s2", "title": "t2", "service": "spotify"}, "status": "review", "amazon_id": ""}, "options": ` + o + `}
	]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeResolve{}
	q := newMQ(t, &fakeFetch{}, r)
	q.Restore(st.Items)
	s := q.Snapshot()
	if it := find(s, "s1"); it.Status != Queued || it.IDs["amazon"] != "B0X" {
		t.Errorf("s1 = %+v", it)
	}
	if it := find(s, "s2"); it.Status != Review || it.ReviewService != "amazon" || len(it.IDs) != 0 {
		t.Errorf("s2 = %+v", it)
	}
	q.Resume()
	waitFor(t, q, allIn(Completed, "s1"))
	if len(r.callLog()) != 0 {
		t.Errorf("resolve calls = %v", r.callLog())
	}
}
