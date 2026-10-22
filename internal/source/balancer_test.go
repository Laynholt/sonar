package source

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

type fake struct {
	name    string
	results []error // consumed in order; empty = success
	calls   int
	onCall  func()
}

func (f *fake) Name() string { return f.name }
func (f *fake) Download(ctx context.Context, trackURL, pathNoExt string, onStatus func(string)) (string, error) {
	f.calls++
	if f.onCall != nil {
		f.onCall()
	}
	if len(f.results) > 0 {
		err := f.results[0]
		f.results = f.results[1:]
		if err != nil {
			return "", err
		}
	}
	return f.name + ".flac", nil
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTest(srcs ...Source) (*Balancer, *clock) {
	b := NewBalancer(5*time.Minute, srcs...)
	c := &clock{t: time.Unix(1000, 0)}
	b.now = c.now
	return b, c
}

func fetch(b *Balancer, name string) (string, error) {
	return b.Fetch(context.Background(), name, "u", "p", nil)
}

func mustFetch(t *testing.T, b *Balancer, name string) {
	t.Helper()
	if _, err := fetch(b, name); err != nil {
		t.Fatal(err)
	}
}

// changes counts OnChange calls. It calls Status inside, so OnChange under the lock would deadlock.
func changes(b *Balancer) chan struct{} {
	ch := make(chan struct{}, 100)
	b.OnChange = func() {
		b.Status()
		ch <- struct{}{}
	}
	return ch
}

func waitChange(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("OnChange not called")
	}
}

func noChange(t *testing.T, ch chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("unexpected OnChange")
	default:
	}
}

func status(b *Balancer, name string) Status {
	i := slices.IndexFunc(b.Status(), func(s Status) bool { return s.Name == name })
	return b.Status()[i]
}

func TestNames(t *testing.T) {
	b, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	if got := b.Names(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Names = %v", got)
	}
	if !b.Has("a") || b.Has("zzz") || b.Ready("zzz") {
		t.Fatal("Has/Ready on an unknown site")
	}
}

func TestFetchCooldownOnUnavailable(t *testing.T) {
	a := &fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}
	bal, clk := newTest(a, &fake{name: "b"})
	if _, err := fetch(bal, "a"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if bal.Ready("a") || !bal.Ready("b") {
		t.Fatal("a should be paused, b ready")
	}
	if st := status(bal, "a"); st.Available || !strings.Contains(st.Reason, "down") {
		t.Fatalf("status %+v", st)
	}
	clk.t = clk.t.Add(10 * time.Minute)
	if !bal.Ready("a") || !status(bal, "a").Available {
		t.Fatal("a should be ready after the cooldown")
	}
}

func TestNeedsCheckUntilReset(t *testing.T) {
	bal, clk := newTest(&fake{name: "a", results: []error{ErrNeedsCheck}}, &fake{name: "b"})
	ch := changes(bal)
	fetch(bal, "a")
	clk.t = clk.t.Add(time.Hour)
	if st := status(bal, "a"); bal.Ready("a") || !st.NeedsCheck || st.Available {
		t.Fatalf("status %+v", st)
	}
	noChange(t, ch)
	bal.Reset("a")
	waitChange(t, ch)
	if !bal.Ready("a") {
		t.Fatal("a not ready after Reset")
	}
}

func TestCooldownDoesNotLiftCheck(t *testing.T) {
	down := fmt.Errorf("%w: down", ErrUnavailable)
	bal, clk := newTest(&fake{name: "a", results: []error{ErrNeedsCheck, down}})
	fetch(bal, "a")
	fetch(bal, "a") // the caller ignored Ready: the cooldown must not replace the check
	clk.t = clk.t.Add(10 * time.Minute)
	if st := status(bal, "a"); !st.NeedsCheck || bal.Ready("a") {
		t.Fatalf("status %+v", st)
	}
}

func TestNeedCheck(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"})
	bal.NeedCheck("a")
	if st := status(bal, "a"); !st.NeedsCheck || st.Available || bal.Ready("a") || !strings.Contains(st.Reason, "Settings") {
		t.Fatalf("status %+v", st)
	}
	bal.Reset("a")
	if !bal.Ready("a") {
		t.Error("Reset did not lift NeedCheck")
	}
}

// Usable is false only while a site waits for the user's browser check; a cooldown or the guard ends without them.
func TestUsable(t *testing.T) {
	bal, _ := newTest(&fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}, &fake{name: "b"})
	fetch(bal, "a")
	bal.SetGuard(1)
	mustFetch(t, bal, "b")
	if bal.Ready("a") || bal.Ready("b") || !bal.Usable("a") || !bal.Usable("b") {
		t.Fatal("a cooling down and b asking must stay usable")
	}
	bal.NeedCheck("a")
	if bal.Usable("a") || bal.Usable("nope") {
		t.Fatal("a waiting for a check, or an unknown site, is usable")
	}
	bal.Reset("a")
	if !bal.Usable("a") {
		t.Fatal("a not usable after Reset")
	}
}

func TestFetchSuccessClearsPause(t *testing.T) {
	bal, _ := newTest(&fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}})
	fetch(bal, "a")
	ch := changes(bal)
	mustFetch(t, bal, "a") // Fetch does not gate: the caller asked for a paused site
	waitChange(t, ch)
	if !bal.Ready("a") {
		t.Fatal("a should be ready after a success")
	}
}

func TestFetchErrors(t *testing.T) {
	a := &fake{name: "a", results: []error{fmt.Errorf("a: HTTP 503: %w", ErrUnavailable)}}
	b := &fake{name: "b", results: []error{errors.New("boom")}}
	bal, _ := newTest(a, b)
	if _, err := fetch(bal, "a"); err == nil || err.Error() != "a: HTTP 503: site unavailable" || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, err := fetch(bal, "b"); err == nil || err.Error() != "b: boom" {
		t.Fatalf("err = %v", err)
	}
	if !bal.Ready("b") {
		t.Fatal("a track error must not pause the site")
	}
	if _, err := fetch(bal, "zzz"); err == nil || !strings.Contains(err.Error(), "zzz") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bal, _ := newTest(&fake{name: "a", results: []error{fmt.Errorf("%w: x", ErrUnavailable)}, onCall: cancel})
	if _, err := bal.Fetch(ctx, "a", "u", "p", nil); !errors.Is(err, context.Canceled) || !bal.Ready("a") {
		t.Fatalf("err=%v ready=%v", err, bal.Ready("a"))
	}
}

type statusSrc struct{ fake }

func (s *statusSrc) Download(ctx context.Context, u, p string, onStatus func(string)) (string, error) {
	onStatus("Ripping")
	return "x", nil
}

func TestFetchPrefixesStatus(t *testing.T) {
	bal, _ := newTest(&statusSrc{fake{name: "a"}})
	var got string
	if _, err := bal.Fetch(context.Background(), "a", "u", "p", func(s string) { got = s }); err != nil || got != "a: Ripping" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestFetchCountsPerSite(t *testing.T) {
	bal, _ := newTest(&fake{name: "a", results: []error{errors.New("x"), nil, nil}}, &fake{name: "b"})
	fetch(bal, "a") // a failure does not count
	mustFetch(t, bal, "a")
	mustFetch(t, bal, "a")
	mustFetch(t, bal, "b")
	if a, b := status(bal, "a"), status(bal, "b"); a.Count != 2 || b.Count != 1 {
		t.Fatalf("counts %d %d", a.Count, b.Count)
	}
}

func TestLimitAsksOnlyThatSite(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	ch := changes(bal)
	bal.SetGuard(2)
	mustFetch(t, bal, "a")
	mustFetch(t, bal, "b")
	noChange(t, ch)
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	if st := status(bal, "a"); bal.Ready("a") || !st.Asking || st.Count != 2 || st.Limit != 2 || st.PausedUntil != 0 {
		t.Fatalf("a: ready=%v %+v", bal.Ready("a"), st)
	}
	if st := status(bal, "b"); !bal.Ready("b") || st.Asking || st.Count != 1 {
		t.Fatalf("b: %+v", st)
	}
	mustFetch(t, bal, "a") // past the limit while asking: no second notice
	noChange(t, ch)
}

func TestGuardRestResetsCount(t *testing.T) {
	bal, clk := newTest(&fake{name: "a"})
	bal.SetGuard(3)
	for range 3 { // the site rests between successes, so it never builds up
		mustFetch(t, bal, "a")
		clk.t = clk.t.Add(GuardRest)
	}
	if st := status(bal, "a"); st.Asking || st.Count != 1 {
		t.Fatalf("status %+v", st)
	}
}

func TestGuardOff(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"})
	ch := changes(bal)
	bal.SetGuard(0)
	for range 5 {
		mustFetch(t, bal, "a")
	}
	if !bal.Ready("a") {
		t.Fatal("asked with the guard off")
	}
	bal.SetGuard(3) // switched on past the current count
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	if !status(bal, "a").Asking {
		t.Fatal("not asking past the limit")
	}
	bal.SetGuard(-1) // switched off while asking
	waitChange(t, ch)
	if !bal.Ready("a") {
		t.Fatal("still asking with the guard off")
	}
}

func TestRaisingLimitClearsAsking(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	ch := changes(bal)
	bal.SetGuard(2)
	mustFetch(t, bal, "a")
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	bal.SetGuard(2) // same limit: still asking
	noChange(t, ch)
	if !status(bal, "a").Asking {
		t.Fatal("stopped asking at the same limit")
	}
	bal.SetGuard(3)
	waitChange(t, ch)
	noChange(t, ch)
	if st := status(bal, "a"); !bal.Ready("a") || st.Asking || st.Count != 2 {
		t.Fatalf("after raising the limit: %+v", st)
	}
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	if !status(bal, "a").Asking {
		t.Fatal("not asking at the new limit")
	}
}

func TestGuardWaitPausesThenResumes(t *testing.T) {
	bal, clk := newTest(&fake{name: "a"}, &fake{name: "b"})
	ch := changes(bal)
	bal.SetGuard(1)
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	bal.GuardWait("b", 200*time.Millisecond) // not asking: no-op
	noChange(t, ch)
	bal.GuardWait("a", 200*time.Millisecond)
	waitChange(t, ch)
	st := status(bal, "a")
	if want := clk.t.Add(200 * time.Millisecond).UnixMilli(); st.PausedUntil != want || st.Asking || st.Count != 1 || bal.Ready("a") {
		t.Fatalf("status %+v, want paused until %d", st, want)
	}
	if p := bal.GuardPauses(); len(p) != 1 || !p["a"].Equal(clk.t.Add(200*time.Millisecond)) {
		t.Fatalf("GuardPauses = %v", p)
	}
	waitChange(t, ch) // the pause ends
	if st := status(bal, "a"); !bal.Ready("a") || st.Count != 0 || st.PausedUntil != 0 {
		t.Fatalf("after the wait: %+v", st)
	}
	if len(bal.GuardPauses()) != 0 {
		t.Fatal("pause still listed")
	}
}

func TestGuardContinueResetsCount(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"})
	ch := changes(bal)
	bal.SetGuard(2)
	mustFetch(t, bal, "a")
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	bal.GuardContinue("a")
	waitChange(t, ch)
	if st := status(bal, "a"); !bal.Ready("a") || st.Count != 0 || st.Asking {
		t.Fatalf("after continue from asking: %+v", st)
	}

	mustFetch(t, bal, "a")
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	bal.GuardWait("a", time.Hour)
	waitChange(t, ch)
	bal.GuardContinue("a") // "Resume now" during the pause
	waitChange(t, ch)
	if st := status(bal, "a"); !bal.Ready("a") || st.Count != 0 || st.PausedUntil != 0 {
		t.Fatalf("after continue from a pause: %+v", st)
	}
	bal.GuardContinue("a") // nothing to continue
	noChange(t, ch)
}

func TestCooldownEndsCallsOnChange(t *testing.T) {
	a := &fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}
	bal := NewBalancer(200*time.Millisecond, a) // the real clock, so the cooldown really ends
	ch := changes(bal)
	fetch(bal, "a")
	if bal.Ready("a") {
		t.Fatal("a should be cooling down")
	}
	waitChange(t, ch)
	if !bal.Ready("a") {
		t.Fatal("a not ready when OnChange fired")
	}
}

func TestResetStopsTimers(t *testing.T) {
	a := &fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}
	bal, _ := newTest(a)
	bal.cooldown = 200 * time.Millisecond
	ch := changes(bal)
	bal.SetGuard(1)
	fetch(bal, "a")
	bal.Reset("a")
	waitChange(t, ch)
	time.Sleep(300 * time.Millisecond)
	noChange(t, ch) // the cooldown timer was stopped

	mustFetch(t, bal, "a")
	waitChange(t, ch) // asking
	bal.GuardWait("a", 200*time.Millisecond)
	waitChange(t, ch)
	bal.GuardContinue("a")
	waitChange(t, ch)
	time.Sleep(300 * time.Millisecond)
	noChange(t, ch) // the guard timer was stopped
}

func TestRestoreGuardPauses(t *testing.T) {
	bal, clk := newTest(&fake{name: "a"}, &fake{name: "b"})
	ch := changes(bal)
	bal.RestoreGuardPauses(map[string]time.Time{"b": clk.t.Add(-time.Second)}) // only a past pause: nothing to arm
	time.Sleep(50 * time.Millisecond)
	noChange(t, ch)
	if !bal.Ready("b") || len(bal.GuardPauses()) != 0 {
		t.Fatalf("past pause restored: %+v", status(bal, "b"))
	}

	until := clk.t.Add(200 * time.Millisecond)
	bal.RestoreGuardPauses(map[string]time.Time{"a": until, "b": clk.t.Add(-time.Second), "zzz": until})
	waitChange(t, ch) // exactly one for the restore
	time.Sleep(50 * time.Millisecond)
	noChange(t, ch) // the past entry fired nothing
	if bal.Ready("a") || status(bal, "a").PausedUntil != until.UnixMilli() {
		t.Fatalf("a: %+v", status(bal, "a"))
	}
	if !bal.Ready("b") || status(bal, "b").PausedUntil != 0 {
		t.Fatalf("b: %+v", status(bal, "b"))
	}
	if p := bal.GuardPauses(); len(p) != 1 || !p["a"].Equal(until) {
		t.Fatalf("GuardPauses = %v", p)
	}
	waitChange(t, ch) // exactly one for the end of a's pause
	if !bal.Ready("a") {
		t.Fatal("a not ready after its pause")
	}
	time.Sleep(300 * time.Millisecond)
	noChange(t, ch) // b armed no timer
}
