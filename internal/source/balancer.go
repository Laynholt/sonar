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

// site is one source's state. Its fields are guarded by Balancer.mu.
type site struct {
	pause      pause
	coolTimer  *time.Timer // calls OnChange when the cooldown ends
	count      int         // successful downloads this session, for the guard
	last       time.Time   // of the last success
	asking     bool        // at the guard limit, waiting for GuardWait or GuardContinue
	guardUntil time.Time   // end of a guard pause, zero for none; cleared only by its timer or GuardContinue
	guardTimer *time.Timer
}

// Balancer gates and counts downloads per site: it pauses a site that fails, and stops one that
// reached the guard limit until the user answers. Picking a ready site is the caller's job.
type Balancer struct {
	sources  []Source
	cooldown time.Duration
	now      func() time.Time
	rest     time.Duration // a site idle this long starts counting again

	// OnChange is called, outside any lock, when a site may have become ready or its guard state changed. Set it before use.
	OnChange func()

	mu    sync.Mutex // guards the fields below and every site's
	sites map[string]*site
	limit int // 0 or less = guard off
	wait  time.Duration
}

// GuardRest is how long a site must go unused before its count restarts.
const GuardRest = 30 * time.Minute

func NewBalancer(cooldown time.Duration, sources ...Source) *Balancer {
	b := &Balancer{sources: sources, cooldown: cooldown, now: time.Now, rest: GuardRest, sites: map[string]*site{}}
	for _, s := range sources {
		b.sites[s.Name()] = &site{}
	}
	return b
}

// active reports whether p still keeps its site out of rotation. Caller holds mu.
func (b *Balancer) active(p pause) bool { return p.needsCheck || b.now().Before(p.until) }

func (b *Balancer) changed() {
	if b.OnChange != nil {
		b.OnChange()
	}
}

func stopTimer(t **time.Timer) {
	if *t != nil {
		(*t).Stop()
		*t = nil
	}
}

func (b *Balancer) Names() []string {
	names := make([]string, len(b.sources))
	for i, s := range b.sources {
		names[i] = s.Name()
	}
	return names
}

// Ready reports whether name may take a track: no cooldown, no check, not asking, no guard pause.
func (b *Balancer) Ready(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sites[name]
	return s != nil && !b.active(s.pause) && !s.asking && s.guardUntil.IsZero()
}

// Usable reports whether name can become Ready without the user: false only while it waits for a browser check.
func (b *Balancer) Usable(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sites[name]
	return s != nil && !s.pause.needsCheck
}

// Fetch downloads through name whether or not it is Ready, and updates its count and pauses.
func (b *Balancer) Fetch(ctx context.Context, name, trackURL, pathNoExt string, onStatus func(string)) (string, error) {
	i := slices.IndexFunc(b.sources, func(s Source) bool { return s.Name() == name })
	if i < 0 {
		return "", fmt.Errorf("unknown source %q", name)
	}
	var st func(string)
	if onStatus != nil {
		st = func(m string) { onStatus(name + ": " + m) }
	}
	path, err := b.sources[i].Download(ctx, trackURL, pathNoExt, st)
	if err == nil {
		b.succeed(name)
		return path, nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return "", cerr
	}
	b.punish(name, err)
	if !strings.HasPrefix(err.Error(), name+": ") { // the clients usually prefix their own errors
		err = fmt.Errorf("%s: %w", name, err)
	}
	return "", err
}

// succeed lifts a pause, since the site demonstrably works, and counts the download for the guard.
func (b *Balancer) succeed(name string) {
	b.mu.Lock()
	s := b.sites[name]
	changed := b.active(s.pause)
	s.pause = pause{}
	stopTimer(&s.coolTimer)
	now := b.now()
	if now.Sub(s.last) >= b.rest {
		s.count = 0
	}
	s.count++
	s.last = now
	if b.limit > 0 && s.count >= b.limit && !s.asking && s.guardUntil.IsZero() {
		s.asking, changed = true, true
	}
	b.mu.Unlock()
	if changed {
		b.changed()
	}
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
	s := b.sites[name]
	if s == nil || s.pause.needsCheck {
		return // only Reset lifts a browser check, not a cooldown
	}
	s.pause = p
	stopTimer(&s.coolTimer)
	if until := p.until; !until.IsZero() {
		s.coolTimer = time.AfterFunc(until.Sub(b.now()), func() {
			b.mu.Lock()
			current := s.pause.until.Equal(until) // not superseded by a check or a later cooldown
			b.mu.Unlock()
			if current {
				b.changed()
			}
		})
	}
}

// SetGuard sets how many successful downloads one site may serve before it asks, and how long
// GuardWait pauses it. A limit of 0 or less turns the guard off; a site asking below the new limit stops asking.
func (b *Balancer) SetGuard(limit int, wait time.Duration) {
	b.mu.Lock()
	b.limit, b.wait = limit, wait
	changed := false
	for _, s := range b.sites {
		if s.asking && (limit <= 0 || s.count < limit) {
			s.asking, changed = false, true
		}
	}
	b.mu.Unlock()
	if changed {
		b.changed()
	}
}

// pauseGuard pauses s until until; at the end its count restarts. Caller holds mu.
func (b *Balancer) pauseGuard(s *site, until time.Time) {
	stopTimer(&s.guardTimer)
	s.guardUntil = until
	s.guardTimer = time.AfterFunc(until.Sub(b.now()), func() {
		b.mu.Lock()
		current := s.guardUntil.Equal(until) // not lifted or replaced meanwhile
		if current {
			s.guardUntil, s.guardTimer, s.count = time.Time{}, nil, 0
		}
		b.mu.Unlock()
		if current {
			b.changed()
		}
	})
}

// GuardWait is "Wait" on an asking site: it pauses for the guard wait, then counts from 0 again.
func (b *Balancer) GuardWait(name string) {
	b.mu.Lock()
	s := b.sites[name]
	if s == nil || !s.asking {
		b.mu.Unlock()
		return
	}
	s.asking = false
	b.pauseGuard(s, b.now().Add(b.wait))
	b.mu.Unlock()
	b.changed()
}

// GuardContinue is "Continue anyway", and "Resume now" during a guard pause.
func (b *Balancer) GuardContinue(name string) {
	b.mu.Lock()
	s := b.sites[name]
	if s == nil || (!s.asking && s.guardUntil.IsZero()) {
		b.mu.Unlock()
		return
	}
	s.asking, s.guardUntil, s.count = false, time.Time{}, 0
	stopTimer(&s.guardTimer)
	b.mu.Unlock()
	b.changed()
}

// GuardPauses lists the unfinished guard pauses, to be saved and given back to RestoreGuardPauses.
func (b *Balancer) GuardPauses() map[string]time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]time.Time{}
	for name, s := range b.sites {
		if !s.guardUntil.IsZero() {
			out[name] = s.guardUntil
		}
	}
	return out
}

// RestoreGuardPauses re-arms saved guard pauses at their absolute end; past ones and unknown sites are ignored.
func (b *Balancer) RestoreGuardPauses(p map[string]time.Time) {
	b.mu.Lock()
	now, changed := b.now(), false
	for name, until := range p {
		if s := b.sites[name]; s != nil && until.After(now) {
			b.pauseGuard(s, until)
			changed = true
		}
	}
	b.mu.Unlock()
	if changed {
		b.changed()
	}
}

// Status lists the sources in constructor order.
func (b *Balancer) Status() []Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Status, 0, len(b.sources))
	for _, src := range b.sources {
		s := b.sites[src.Name()]
		st := Status{Name: src.Name(), Available: true, Count: s.count, Limit: b.limit, Asking: s.asking}
		if b.active(s.pause) {
			st.Available, st.NeedsCheck, st.Reason = false, s.pause.needsCheck, s.pause.reason
		}
		if !s.guardUntil.IsZero() {
			st.PausedUntil = s.guardUntil.UnixMilli()
		}
		out = append(out, st)
	}
	return out
}

// NeedCheck pauses a source until Reset, e.g. doubledouble before its first browser check.
func (b *Balancer) NeedCheck(name string) {
	b.punish(name, fmt.Errorf("%s: %w", name, ErrNeedsCheck))
}

// Reset lifts a cooldown or check, e.g. after the user passed the browser check. A guard pause stays.
func (b *Balancer) Reset(name string) {
	b.mu.Lock()
	if s := b.sites[name]; s != nil {
		s.pause = pause{}
		stopTimer(&s.coolTimer)
	}
	b.mu.Unlock()
	b.changed()
}

func (b *Balancer) Has(name string) bool {
	return slices.ContainsFunc(b.sources, func(s Source) bool { return s.Name() == name })
}
