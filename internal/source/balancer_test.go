package source

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"sonar/internal/download"
)

type fake struct {
	name    string
	results []error // consumed in order; empty = success
	calls   int
	onCall  func()
}

func (f *fake) Name() string { return f.name }
func (f *fake) Download(ctx context.Context, r Request) (string, error) {
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
	b := NewBalancer(5 * time.Minute)
	for _, s := range srcs {
		b.Add(s, "amazon", Guard{})
	}
	c := &clock{t: time.Unix(1000, 0)}
	b.now = c.now
	return b, c
}

func fetch(b *Balancer, name string) (string, error) {
	return b.Fetch(context.Background(), name, Request{PathNoExt: "p"})
}

// guardAll gives every site the same limit, as the one global guard did.
func guardAll(b *Balancer, limit int) {
	for _, n := range b.Names() {
		b.SetGuard(n, Guard{Limit: limit})
	}
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
	if !b.Has("a") || b.Has("zzz") || b.Ready("zzz") || b.Accepts("zzz") != "" {
		t.Fatal("Has/Ready/Accepts on an unknown site")
	}
	b.Add(&fake{name: "c"}, "any", Guard{})
	if got := b.Names(); !slices.Equal(got, []string{"a", "b", "c"}) || b.Accepts("a") != "amazon" || b.Accepts("c") != "any" {
		t.Fatalf("Names = %v, accepts %q %q", got, b.Accepts("a"), b.Accepts("c"))
	}
}

// Add on a known name swaps its Source, accepts and guard, but keeps its place and its count.
func TestAddReplacesKeepsCounters(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	mustFetch(t, bal, "a")
	ch := changes(bal)
	repl := &fake{name: "a"}
	bal.Add(repl, "any", Guard{Limit: 2})
	waitChange(t, ch)
	mustFetch(t, bal, "a")
	if st := status(bal, "a"); repl.calls != 1 || st.Count != 2 || !st.Asking || st.Accepts != "any" || st.Label != "a" {
		t.Fatalf("calls %d, status %+v", repl.calls, st)
	}
	if got := bal.Names(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Names = %v", got)
	}
}

type labeled struct{ fake }

func (l *labeled) Label() string { return "Site A" }

func TestStatusLabel(t *testing.T) {
	bal, _ := newTest(&labeled{fake{name: "a"}})
	if st := status(bal, "a"); st.Label != "Site A" || st.Accepts != "amazon" {
		t.Fatalf("status %+v", st)
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
	if st := status(bal, "a"); bal.Ready("a") || !st.NeedsAction || st.Available {
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
	if st := status(bal, "a"); !st.NeedsAction || bal.Ready("a") {
		t.Fatalf("status %+v", st)
	}
}

func TestNeedAction(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"})
	ch := changes(bal)
	bal.NeedAction("a", "a: log in first", "login")
	waitChange(t, ch)
	if st := status(bal, "a"); !st.NeedsAction || st.Available || bal.Ready("a") || bal.Usable("a") || st.Reason != "a: log in first" || st.Action != "login" {
		t.Fatalf("status %+v", st)
	}
	bal.NeedAction("a", "a: still not logged in", "login") // a newer reason replaces the old
	waitChange(t, ch)
	if st := status(bal, "a"); st.Reason != "a: still not logged in" {
		t.Fatalf("status %+v", st)
	}
	bal.NeedAction("zzz", "x", "y") // unknown: ignored
	bal.Reset("a")
	if st := status(bal, "a"); !bal.Ready("a") || st.Action != "" {
		t.Errorf("Reset did not lift NeedAction: %+v", st)
	}
}

// actionErr is a needs-action error naming its settings action, as the plugin adapter returns.
type actionErr struct{}

func (actionErr) Error() string  { return "a: expired" }
func (actionErr) Unwrap() error  { return ErrNeedsCheck }
func (actionErr) Action() string { return "check" }

func TestFetchNeedsActionKeepsAction(t *testing.T) {
	bal, _ := newTest(&fake{name: "a", results: []error{actionErr{}}})
	if _, err := fetch(bal, "a"); err == nil || err.Error() != "a: expired" {
		t.Fatalf("err = %v", err)
	}
	if st := status(bal, "a"); !st.NeedsAction || st.Action != "check" || st.Reason != "a: expired" {
		t.Fatalf("status %+v", st)
	}
}

// Usable is false only while a site waits for the user's browser check; a cooldown or the guard ends without them.
func TestUsable(t *testing.T) {
	bal, _ := newTest(&fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}, &fake{name: "b"})
	fetch(bal, "a")
	guardAll(bal, 1)
	mustFetch(t, bal, "b")
	if bal.Ready("a") || bal.Ready("b") || !bal.Usable("a") || !bal.Usable("b") {
		t.Fatal("a cooling down and b asking must stay usable")
	}
	bal.NeedAction("a", "a: check", "check")
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
	if _, err := bal.Fetch(ctx, "a", Request{}); !errors.Is(err, context.Canceled) || !bal.Ready("a") {
		t.Fatalf("err=%v ready=%v", err, bal.Ready("a"))
	}
}

type statusSrc struct{ fake }

func (s *statusSrc) Download(ctx context.Context, r Request) (string, error) {
	r.OnStatus("Ripping")
	return "x", nil
}

func TestFetchPrefixesStatus(t *testing.T) {
	bal, _ := newTest(&statusSrc{fake{name: "a"}})
	var got string
	if _, err := bal.Fetch(context.Background(), "a", Request{OnStatus: func(s string) { got = s }}); err != nil || got != "a: Ripping" {
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
	guardAll(bal, 2)
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

// Each site has its own guard: a at limit 2 asks, b with the guard off never does.
func TestGuardPerSite(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	bal.SetGuard("a", Guard{Limit: 2})
	bal.SetGuard("b", Guard{Limit: 0})
	bal.SetGuard("zzz", Guard{Limit: 1}) // unknown: ignored
	for range 2 {
		mustFetch(t, bal, "a")
	}
	for range 5 {
		mustFetch(t, bal, "b")
	}
	if st := status(bal, "a"); !st.Asking || bal.Ready("a") || st.Limit != 2 {
		t.Fatalf("a: %+v", st)
	}
	if st := status(bal, "b"); st.Asking || !bal.Ready("b") || st.Limit != 0 || st.Count != 5 {
		t.Fatalf("b: %+v", st)
	}
}

func TestStatusWaits(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	waits := []int{15, 30, 60}
	bal.SetGuard("a", Guard{Limit: 50, Waits: waits, Wait: 30})
	waits[0] = 99 // the balancer keeps its own copy
	if st := status(bal, "a"); !slices.Equal(st.Waits, []int{15, 30, 60}) || st.Wait != 30 || st.Limit != 50 {
		t.Fatalf("a: %+v", st)
	}
	if st := status(bal, "b"); st.Waits == nil || len(st.Waits) != 0 || st.Wait != 0 { // [] in JSON, never null
		t.Fatalf("b: %+v", st)
	}
}

// A Source that finds its site off before SetEnabled does (a plugin disabled in the Registry first) gets a
// cooldown, so its worker does not retake the track at once. Switching the site off and on lifts the cooldown,
// but not a needed action.
func TestSourceSiteOffCoolsDown(t *testing.T) {
	off := fmt.Errorf("plugin a: disabled: %w", download.ErrSiteOff)
	bal, _ := newTest(&fake{name: "a", results: []error{off}})
	if _, err := fetch(bal, "a"); !errors.Is(err, download.ErrSiteOff) || bal.Ready("a") || !bal.Usable("a") {
		t.Fatalf("err = %v, ready %v, usable %v", err, bal.Ready("a"), bal.Usable("a"))
	}
	ch := changes(bal)
	bal.SetEnabled("a", false)
	bal.SetEnabled("a", true)
	waitChange(t, ch)
	waitChange(t, ch)
	if st := status(bal, "a"); !bal.Ready("a") || st.Reason != "" {
		t.Fatalf("back on: %+v", st)
	}
	bal.NeedAction("a", "a: check", "check")
	bal.SetEnabled("a", false)
	bal.SetEnabled("a", true)
	if st := status(bal, "a"); bal.Ready("a") || !st.NeedsAction {
		t.Fatalf("a switch must not lift a needed action: %+v", st)
	}
}

// blockSrc closes started in Download, then blocks until its ctx ends.
type blockSrc struct {
	fake
	started chan struct{}
}

func (s *blockSrc) Download(ctx context.Context, _ Request) (string, error) {
	s.calls++
	close(s.started)
	<-ctx.Done()
	return "", ctx.Err()
}

// Switching a site off cancels its running download with an error the queue requeues on (download.ErrSiteOff),
// without punishing the site. While off it is neither Ready nor Usable, is left out of Status and downloads nothing.
func TestDisableRequeuesRunning(t *testing.T) {
	a := &blockSrc{fake: fake{name: "a"}, started: make(chan struct{})}
	bal, _ := newTest(a, &fake{name: "b"})
	ch := changes(bal)
	errc := make(chan error, 1)
	go func() {
		_, err := fetch(bal, "a")
		errc <- err
	}()
	<-a.started
	bal.SetEnabled("a", false)
	waitChange(t, ch)
	var err error
	select {
	case err = <-errc:
	case <-time.After(5 * time.Second):
		t.Fatal("Fetch not cancelled")
	}
	if !errors.Is(err, download.ErrSiteOff) || !errors.Is(err, download.ErrSiteDown) || !strings.HasPrefix(err.Error(), "a: ") {
		t.Fatalf("err = %v", err)
	}
	if bal.Ready("a") || bal.Usable("a") || !bal.Ready("b") {
		t.Fatal("a must be neither ready nor usable while off, b untouched")
	}
	if st := bal.Status(); len(st) != 1 || st[0].Name != "b" {
		t.Fatalf("Status = %+v", st)
	}
	bal.SetEnabled("a", false) // already off
	noChange(t, ch)
	if _, err := fetch(bal, "a"); !errors.Is(err, download.ErrSiteOff) || a.calls != 1 {
		t.Fatalf("fetch while off: err = %v, calls %d", err, a.calls)
	}

	bal.SetEnabled("a", true)
	waitChange(t, ch)
	if st := status(bal, "a"); !bal.Ready("a") || !bal.Usable("a") || st.Reason != "" || !st.Available {
		t.Fatalf("a back on: %+v", st)
	}
	a.started = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-a.started; cancel() }()
	// cancelled by its caller after the site is back on: the plain cancel, not a site-down
	if _, err := bal.Fetch(ctx, "a", Request{}); !errors.Is(err, context.Canceled) || errors.Is(err, download.ErrSiteDown) {
		t.Fatalf("a cancelled by its caller: err = %v", err)
	}
}

func TestGuardRestResetsCount(t *testing.T) {
	bal, clk := newTest(&fake{name: "a"})
	guardAll(bal, 3)
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
	guardAll(bal, 0)
	for range 5 {
		mustFetch(t, bal, "a")
	}
	if !bal.Ready("a") {
		t.Fatal("asked with the guard off")
	}
	guardAll(bal, 3) // switched on past the current count
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	if !status(bal, "a").Asking {
		t.Fatal("not asking past the limit")
	}
	guardAll(bal, -1) // switched off while asking
	waitChange(t, ch)
	if !bal.Ready("a") {
		t.Fatal("still asking with the guard off")
	}
}

func TestRaisingLimitClearsAsking(t *testing.T) {
	bal, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	ch := changes(bal)
	guardAll(bal, 2)
	mustFetch(t, bal, "a")
	mustFetch(t, bal, "a")
	waitChange(t, ch)
	guardAll(bal, 2) // same limit: still asking
	noChange(t, ch)
	if !status(bal, "a").Asking {
		t.Fatal("stopped asking at the same limit")
	}
	guardAll(bal, 3)
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
	guardAll(bal, 1)
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
	guardAll(bal, 2)
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
	bal := NewBalancer(200 * time.Millisecond) // the real clock, so the cooldown really ends
	bal.Add(a, "amazon", Guard{})
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
	guardAll(bal, 1)
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
