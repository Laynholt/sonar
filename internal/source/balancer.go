package source

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
)

type pause struct {
	until      time.Time // zero while waiting for Reset
	needsCheck bool
	reason     string
}

// Balancer spreads downloads round-robin over its sources and pauses the ones that fail.
type Balancer struct {
	sources  []Source
	cooldown time.Duration
	now      func() time.Time

	// OnLimit is called, outside any lock, when a site's count is at or past the guard limit. Set it before use.
	OnLimit func(site string, count int)
	rest    time.Duration // a site idle this long starts counting again

	mu     sync.Mutex // guards the fields below
	next   int
	paused map[string]pause
	limit  int // 0 = guard off
	count  map[string]int
	last   map[string]time.Time
}

// GuardRest is how long a site must go unused before its count restarts.
const GuardRest = 30 * time.Minute

func NewBalancer(cooldown time.Duration, sources ...Source) *Balancer {
	return &Balancer{sources: sources, cooldown: cooldown, now: time.Now, paused: map[string]pause{},
		rest: GuardRest, count: map[string]int{}, last: map[string]time.Time{}}
}

// active reports whether p still keeps its site out of rotation. Caller holds mu.
func (b *Balancer) active(p pause) bool { return p.needsCheck || b.now().Before(p.until) }

func (b *Balancer) Download(ctx context.Context, mode, trackURL, pathNoExt string, onStatus func(string)) (string, error) {
	var tries []Source
	if mode == Auto || mode == "" {
		b.mu.Lock()
		var free []Source
		all := slices.Clone(b.sources)
		if len(all) > 0 {
			start := b.next % len(all)
			b.next++
			all = slices.Concat(all[start:], all[:start])
		}
		for _, s := range all {
			if !b.active(b.paused[s.Name()]) {
				free = append(free, s)
			}
		}
		b.mu.Unlock()
		// With every site paused, still try them all rather than fail without a request.
		if tries = free; len(tries) == 0 {
			tries = all
		}
		if len(tries) == 0 {
			return "", errors.New("no download sources configured")
		}
	} else {
		i := slices.IndexFunc(b.sources, func(s Source) bool { return s.Name() == mode })
		if i < 0 {
			return "", fmt.Errorf("unknown source %q", mode)
		}
		tries = b.sources[i : i+1]
	}

	var msgs []string
	var errs []any
	for _, s := range tries {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		name := s.Name()
		var st func(string)
		if onStatus != nil {
			st = func(m string) { onStatus(name + ": " + m) }
		}
		path, err := s.Download(ctx, trackURL, pathNoExt, st)
		if err == nil {
			b.Reset(name) // it demonstrably works, e.g. in fixed mode or the all-paused fallback
			b.countSuccess(name)
			return path, nil
		}
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		b.punish(name, err)
		if !strings.HasPrefix(err.Error(), name+": ") { // the clients usually prefix their own errors
			err = fmt.Errorf("%s: %w", name, err)
		}
		msgs = append(msgs, "%w")
		errs = append(errs, err)
	}
	return "", fmt.Errorf(strings.Join(msgs, "; "), errs...)
}

func (b *Balancer) countSuccess(name string) {
	b.mu.Lock()
	now := b.now()
	if now.Sub(b.last[name]) >= b.rest {
		b.count[name] = 0
	}
	b.count[name]++
	b.last[name] = now
	n, fire := b.count[name], b.limit > 0 && b.count[name] >= b.limit
	b.mu.Unlock()
	if fire && b.OnLimit != nil {
		b.OnLimit(name, n)
	}
}

// SetGuard sets how many successful downloads one site may serve before OnLimit fires; 0 turns the guard off.
func (b *Balancer) SetGuard(limit int) {
	b.mu.Lock()
	b.limit = limit
	b.mu.Unlock()
}

func (b *Balancer) ResetCounts() {
	b.mu.Lock()
	clear(b.count)
	b.mu.Unlock()
}

func (b *Balancer) punish(name string, err error) {
	var p pause
	switch {
	case errors.Is(err, ErrNeedsCheck):
		p = pause{needsCheck: true, reason: err.Error()}
	case errors.Is(err, ErrUnavailable):
		p = pause{until: b.now().Add(b.cooldown), reason: err.Error()}
	default:
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.paused[name].needsCheck {
		return // only Reset lifts a browser check, not a cooldown
	}
	b.paused[name] = p
}

// Status lists the sources in constructor order.
func (b *Balancer) Status() []Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Status, 0, len(b.sources))
	for _, s := range b.sources {
		st := Status{Name: s.Name(), Available: true}
		if p := b.paused[s.Name()]; b.active(p) {
			st.Available, st.NeedsCheck, st.Reason = false, p.needsCheck, p.reason
		}
		out = append(out, st)
	}
	return out
}

// NeedCheck pauses a source until Reset, e.g. doubledouble before its first browser check.
func (b *Balancer) NeedCheck(name string) {
	b.punish(name, fmt.Errorf("%s: %w", name, ErrNeedsCheck))
}

// Reset puts a paused source back in rotation, e.g. after the user passed its browser check.
func (b *Balancer) Reset(name string) {
	b.mu.Lock()
	delete(b.paused, name)
	b.mu.Unlock()
}

func (b *Balancer) Has(name string) bool {
	return slices.ContainsFunc(b.sources, func(s Source) bool { return s.Name() == name })
}
