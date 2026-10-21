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

func autoCall(b *Balancer) (string, error) {
	return b.Download(context.Background(), Auto, "u", "p", nil)
}

func TestBalancerRoundRobin(t *testing.T) {
	b, _ := newTest(&fake{name: "a"}, &fake{name: "b"})
	var got []string
	for range 4 {
		p, err := autoCall(b)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, p)
	}
	if want := "a.flac b.flac a.flac b.flac"; strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
}

func TestBalancerFallsBackOnUnavailable(t *testing.T) {
	a := &fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}
	b := &fake{name: "b"}
	bal, clk := newTest(a, b)
	if p, err := autoCall(bal); err != nil || p != "b.flac" {
		t.Fatalf("%q %v", p, err)
	}
	for range 3 {
		autoCall(bal)
	}
	if a.calls != 1 {
		t.Fatalf("a called %d times", a.calls)
	}
	st := bal.Status()
	if st[0].Available || !strings.Contains(st[0].Reason, "down") || !st[1].Available {
		t.Fatalf("status %+v", st)
	}
	clk.t = clk.t.Add(10 * time.Minute)
	if !bal.Status()[0].Available {
		t.Fatal("a should be available after cooldown")
	}
	used := false
	for range 2 {
		if p, _ := autoCall(bal); p == "a.flac" {
			used = true
		}
	}
	if !used {
		t.Fatal("a not used after cooldown")
	}
}

func TestBalancerNeedsCheckUntilReset(t *testing.T) {
	a := &fake{name: "a", results: []error{ErrNeedsCheck}}
	b := &fake{name: "b"}
	bal, clk := newTest(a, b)
	if p, err := autoCall(bal); err != nil || p != "b.flac" {
		t.Fatalf("%q %v", p, err)
	}
	clk.t = clk.t.Add(time.Hour)
	autoCall(bal)
	autoCall(bal)
	if a.calls != 1 {
		t.Fatalf("a called %d times", a.calls)
	}
	if st := bal.Status()[0]; !st.NeedsCheck || st.Available {
		t.Fatalf("status %+v", st)
	}
	bal.Reset("a")
	used := false
	for range 2 {
		if p, _ := autoCall(bal); p == "a.flac" {
			used = true
		}
	}
	if !used {
		t.Fatal("a not used after Reset")
	}
}

func TestBalancerTrackErrorTriesOthers(t *testing.T) {
	a := &fake{name: "a", results: []error{errors.New("not found")}}
	b := &fake{name: "b", results: []error{errors.New("nope")}}
	bal, _ := newTest(a, b)
	_, err := autoCall(bal)
	if err == nil || !strings.Contains(err.Error(), "a: not found") || !strings.Contains(err.Error(), "b: nope") {
		t.Fatalf("err = %v", err)
	}
	if !bal.Status()[0].Available {
		t.Fatal("a should stay available")
	}
}

func TestBalancerFixedMode(t *testing.T) {
	a := &fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}
	b := &fake{name: "b"}
	bal, _ := newTest(a, b)
	_, err := bal.Download(context.Background(), "a", "u", "p", nil)
	if !errors.Is(err, ErrUnavailable) || b.calls != 0 {
		t.Fatalf("err=%v b.calls=%d", err, b.calls)
	}
	if _, err := bal.Download(context.Background(), "zzz", "u", "p", nil); err == nil || !strings.Contains(err.Error(), "zzz") {
		t.Fatalf("err = %v", err)
	}
}

func TestBalancerAllPausedStillTries(t *testing.T) {
	down := fmt.Errorf("%w: down", ErrUnavailable)
	var order []string
	a := &fake{name: "a", results: []error{down, down}, onCall: func() { order = append(order, "a") }}
	b := &fake{name: "b", results: []error{down, down}, onCall: func() { order = append(order, "b") }}
	bal, _ := newTest(a, b)
	if _, err := autoCall(bal); err == nil {
		t.Fatal("want error")
	}
	if bal.Status()[0].Available || bal.Status()[1].Available {
		t.Fatal("both should be paused")
	}
	// next call: round-robin starts at b, but it must still try both
	autoCall(bal)
	if strings.Join(order, "") != "abba" {
		t.Fatalf("order %v", order)
	}
}

func TestBalancerCancelStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	a := &fake{name: "a", results: []error{context.Canceled}, onCall: cancel}
	b := &fake{name: "b"}
	bal, _ := newTest(a, b)
	_, err := bal.Download(ctx, Auto, "u", "p", nil)
	if !errors.Is(err, context.Canceled) || b.calls != 0 || !bal.Status()[0].Available {
		t.Fatalf("err=%v b.calls=%d", err, b.calls)
	}
}

type statusSrc struct{ fake }

func (s *statusSrc) Download(ctx context.Context, u, p string, onStatus func(string)) (string, error) {
	onStatus("Ripping")
	return "x", nil
}

func TestBalancerPrefixesStatus(t *testing.T) {
	bal, _ := newTest(&statusSrc{fake{name: "a"}})
	var got string
	_, err := bal.Download(context.Background(), Auto, "u", "p", func(s string) { got = s })
	if err != nil || got != "a: Ripping" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestBalancerNeedsCheckSurvivesForcedRetry(t *testing.T) {
	down := fmt.Errorf("%w: down", ErrUnavailable)
	a := &fake{name: "a", results: []error{ErrNeedsCheck, down}}
	b := &fake{name: "b", results: []error{down, down}}
	bal, clk := newTest(a, b)
	autoCall(bal) // a needs a check, b is down: everything is paused
	autoCall(bal) // forced retry: a gets ErrUnavailable
	if a.calls != 2 {
		t.Fatalf("a called %d times", a.calls)
	}
	clk.t = clk.t.Add(10 * time.Minute)
	if st := bal.Status()[0]; !st.NeedsCheck || st.Available {
		t.Fatalf("status %+v", st)
	}
	for range 2 {
		if p, err := autoCall(bal); err != nil || p != "b.flac" {
			t.Fatalf("%q %v", p, err)
		}
	}
	if a.calls != 2 {
		t.Fatalf("a called %d times", a.calls)
	}
}

func TestBalancerSuccessClearsPause(t *testing.T) {
	a := &fake{name: "a", results: []error{fmt.Errorf("%w: down", ErrUnavailable)}}
	bal, _ := newTest(a, &fake{name: "b"})
	autoCall(bal)
	if bal.Status()[0].Available {
		t.Fatal("a should be paused")
	}
	if _, err := bal.Download(context.Background(), "a", "u", "p", nil); err != nil {
		t.Fatal(err)
	}
	if !bal.Status()[0].Available {
		t.Fatal("a should be available after a success")
	}
}

func TestBalancerGuardFiresAtLimit(t *testing.T) {
	a, b := &fake{name: "a"}, &fake{name: "b"}
	bal, clk := newTest(a, b)
	type hit struct {
		site  string
		count int
	}
	var hits []hit
	bal.OnLimit = func(site string, count int) { hits = append(hits, hit{site, count}) }
	dl := func(mode string) {
		t.Helper()
		if _, err := bal.Download(context.Background(), mode, "u", "p", nil); err != nil {
			t.Fatal(err)
		}
	}
	bal.SetGuard(3)
	dl("a")
	dl("a")
	dl("b") // counted apart from a
	dl("b")
	if len(hits) != 0 {
		t.Fatalf("fired early: %v", hits)
	}
	dl("a")
	dl("a") // past the limit: no second hit
	if want := []hit{{"a", 3}}; !slices.Equal(hits, want) {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
	dl("b")
	if want := []hit{{"a", 3}, {"b", 3}}; !slices.Equal(hits, want) {
		t.Fatalf("hits = %v, want %v", hits, want)
	}

	hits = nil
	bal.ResetCounts()
	dl("a")
	dl("a")
	dl("a")
	if want := []hit{{"a", 3}}; !slices.Equal(hits, want) {
		t.Fatalf("after ResetCounts hits = %v, want %v", hits, want)
	}

	hits = nil
	bal.ResetCounts()
	for range 3 { // the site rests between successes, so it never builds up
		dl("a")
		clk.t = clk.t.Add(bal.rest)
	}
	if len(hits) != 0 {
		t.Fatalf("fired despite rest: %v", hits)
	}

	bal.SetGuard(0)
	bal.ResetCounts()
	for range 5 {
		dl("a")
	}
	if len(hits) != 0 {
		t.Fatalf("fired with guard off: %v", hits)
	}
}

func TestBalancerGuardIgnoresFailures(t *testing.T) {
	a := &fake{name: "a", results: []error{errors.New("x"), errors.New("x"), nil, nil}}
	bal, _ := newTest(a)
	fired := 0
	bal.OnLimit = func(string, int) { fired++ }
	bal.SetGuard(2)
	for range 4 {
		bal.Download(context.Background(), "a", "u", "p", nil)
	}
	if fired != 1 {
		t.Fatalf("fired = %d, want 1 (only the 2 successes count)", fired)
	}
}
