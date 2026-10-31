package source

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"sonar/internal/download"
)

type pause struct {
	until       time.Time // zero while waiting for Reset
	needsAction bool
	reason      string
	action      string // the settings action that lifts needsAction, "" for none
}

// site is one source's state. Its fields are guarded by Balancer.mu.
type site struct {
	src        Source
	label      string
	accepts    string
	guard      Guard
	on         context.Context // done while the site is switched off; Fetch cancels on it
	turnOff    context.CancelFunc
	pause      pause
	coolTimer  *time.Timer // calls OnChange when the cooldown ends
	count      int         // successful downloads this session, for the guard
	last       time.Time   // of the last success
	asking     bool        // at the guard limit, waiting for GuardWait or GuardContinue
	guardUntil time.Time   // end of a guard pause, zero for none; cleared only by its timer or GuardContinue
	guardTimer *time.Timer
}

func (s *site) enabled() bool { return s.on.Err() == nil }

// Balancer gates and counts downloads per site: it pauses a site that fails, and stops one that
// reached its guard limit until the user answers. Picking a ready site is the caller's job.
type Balancer struct {
	cooldown time.Duration
	now      func() time.Time
	rest     time.Duration // a site idle this long starts counting again

	// OnChange is called, outside any lock, when a site may have become ready or its guard state changed. Set it before use.
	OnChange func()

	mu    sync.Mutex // guards the fields below and every site's
	names []string   // in Add order, or as SetOrder put them
	sites map[string]*site
}

// GuardRest is how long a site must go unused before its count restarts.
const GuardRest = 30 * time.Minute

func NewBalancer(cooldown time.Duration) *Balancer {
	return &Balancer{cooldown: cooldown, now: time.Now, rest: GuardRest, sites: map[string]*site{}}
}

// active reports whether p still keeps its site out of rotation. Caller holds mu.
func (b *Balancer) active(p pause) bool { return p.needsAction || b.now().Before(p.until) }

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

// Add adds an enabled site taking the ids of service accepts ("any" for every track), or, for a known name,
// replaces its Source, accepts and guard while keeping its place, counters, pauses and switch.
// The label in Status is the Source's Label() when it has one, else its name.
func (b *Balancer) Add(s Source, accepts string, g Guard) {
	name, label := s.Name(), s.Name()
	if l, ok := s.(interface{ Label() string }); ok {
		label = l.Label()
	}
	b.mu.Lock()
	st := b.sites[name]
	if st == nil {
		st = &site{}
		st.on, st.turnOff = context.WithCancel(context.Background())
		b.sites[name] = st
		b.names = append(b.names, name)
	}
	st.src, st.label, st.accepts = s, label, accepts
	b.setGuard(st, g)
	b.mu.Unlock()
	b.changed()
}

// SetEnabled switches a site on or off. Off, it is neither Ready nor Usable, Status leaves it out,
// and its running downloads are cancelled with an error that wraps download.ErrSiteOff.
// Switching it on lifts a cooldown, but not a needed action.
func (b *Balancer) SetEnabled(name string, on bool) {
	b.mu.Lock()
	s := b.sites[name]
	if s == nil || s.enabled() == on {
		b.mu.Unlock()
		return
	}
	if on {
		s.on, s.turnOff = context.WithCancel(context.Background())
		if !s.pause.needsAction { // a cooldown, e.g. from the plugin found off before this switch, ends; an action stays
			s.pause = pause{}
			stopTimer(&s.coolTimer)
		}
	} else {
		s.turnOff()
	}
	b.mu.Unlock()
	b.changed()
}

// SetOrder puts the named sites first, in that order; the others follow in their current order.
// Unknown names are ignored.
func (b *Balancer) SetOrder(names []string) {
	b.mu.Lock()
	out := make([]string, 0, len(b.names))
	for _, n := range slices.Concat(names, b.names) {
		if b.sites[n] != nil && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	same := slices.Equal(out, b.names)
	b.names = out
	b.mu.Unlock()
	if !same {
		b.changed()
	}
}

// Names lists the sites, switched off or not, in Add order or the one SetOrder set.
func (b *Balancer) Names() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.names)
}

// Accepts is the service whose ids name downloads, "any" for every track, or "" for an unknown site.
func (b *Balancer) Accepts(name string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.sites[name]; s != nil {
		return s.accepts
	}
	return ""
}

// Ready reports whether name may take a track: on, no cooldown, no action needed, not asking, no guard pause.
func (b *Balancer) Ready(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sites[name]
	return s != nil && s.enabled() && !b.active(s.pause) && !s.asking && s.guardUntil.IsZero()
}

// Usable reports whether name can become Ready without the user: false while it is off or needs an action.
func (b *Balancer) Usable(name string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sites[name]
	return s != nil && s.enabled() && !s.pause.needsAction
}

// Fetch downloads through name whether or not it is Ready, and updates its count and pauses.
// Status texts reach r.OnStatus prefixed with the site's name.
func (b *Balancer) Fetch(ctx context.Context, name string, r Request) (string, error) {
	b.mu.Lock()
	s := b.sites[name]
	var src Source
	var on context.Context
	if s != nil {
		src, on = s.src, s.on
	}
	b.mu.Unlock()
	if s == nil {
		return "", fmt.Errorf("unknown source %q", name)
	}
	switchedOff := func() error { return fmt.Errorf("%s: %w", name, download.ErrSiteOff) }
	if on.Err() != nil {
		return "", switchedOff()
	}
	dctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(on, cancel)()
	if onStatus := r.OnStatus; onStatus != nil {
		r.OnStatus = func(m string) { onStatus(name + ": " + m) }
	}
	path, err := src.Download(dctx, r)
	if err == nil {
		b.succeed(name)
		return path, nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return "", cerr
	}
	if on.Err() != nil { // switched off meanwhile: not the site's fault, so no pause
		return "", switchedOff()
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
	if s.guard.Limit > 0 && s.count >= s.guard.Limit && !s.asking && s.guardUntil.IsZero() {
		s.asking, changed = true, true
	}
	b.mu.Unlock()
	if changed {
		b.changed()
	}
}

// actionOf is the settings action a needs-action error names through an Action method, or "".
func actionOf(err error) string {
	var a interface{ Action() string }
	if errors.As(err, &a) {
		return a.Action()
	}
	return ""
}

// needsReason is what a needs-action error asks of the user: its NeedsMessage, such as a plugin's
// instructions, prefixed with the site's label, else its text.
func needsReason(label string, err error) string {
	var m interface{ NeedsMessage() string }
	if errors.As(err, &m) && m.NeedsMessage() != "" {
		return label + ": " + m.NeedsMessage()
	}
	return err.Error()
}

func (b *Balancer) punish(name string, err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.sites[name]
	if s == nil || s.pause.needsAction {
		return // only Reset lifts a needed action, not a cooldown
	}
	var p pause
	switch {
	case errors.Is(err, ErrNeedsCheck):
		p = pause{needsAction: true, reason: needsReason(s.label, err), action: actionOf(err)}
	case errors.Is(err, ErrUnavailable) || errors.Is(err, download.ErrSiteOff): // off before SetEnabled: no instant retake
		p = pause{until: b.now().Add(b.cooldown), reason: err.Error()}
	default:
		return
	}
	s.pause = p
	stopTimer(&s.coolTimer)
	if until := p.until; !until.IsZero() {
		s.coolTimer = time.AfterFunc(until.Sub(b.now()), func() {
			b.mu.Lock()
			current := s.pause.until.Equal(until) // not superseded by an action or a later cooldown
			b.mu.Unlock()
			if current {
				b.changed()
			}
		})
	}
}

// setGuard sets s's guard and reports whether s stopped asking, being below the new limit. Caller holds mu.
func (b *Balancer) setGuard(s *site, g Guard) bool {
	g.Waits = slices.Clone(g.Waits)
	s.guard = g
	if s.asking && (g.Limit <= 0 || s.count < g.Limit) {
		s.asking = false
		return true
	}
	return false
}

// SetGuard sets how many successful downloads name may serve before it asks, and its pause choices.
// A limit of 0 or less turns the guard off; a site asking below the new limit stops asking.
func (b *Balancer) SetGuard(name string, g Guard) {
	b.mu.Lock()
	changed := false
	if s := b.sites[name]; s != nil {
		changed = b.setGuard(s, g)
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

// GuardWait is "Wait" on an asking site: it pauses for wait, then counts from 0 again.
func (b *Balancer) GuardWait(name string, wait time.Duration) {
	b.mu.Lock()
	s := b.sites[name]
	if s == nil || !s.asking {
		b.mu.Unlock()
		return
	}
	s.asking = false
	b.pauseGuard(s, b.now().Add(wait))
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

// Status lists the sites that are on, in Names order.
func (b *Balancer) Status() []Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Status, 0, len(b.names))
	for _, name := range b.names {
		s := b.sites[name]
		if !s.enabled() {
			continue
		}
		st := Status{Name: name, Label: s.label, Accepts: s.accepts, Available: true, Count: s.count,
			Limit: s.guard.Limit, Waits: append([]int{}, s.guard.Waits...), Wait: s.guard.Wait, Asking: s.asking}
		if b.active(s.pause) {
			st.Available, st.NeedsAction, st.Action, st.Reason = false, s.pause.needsAction, s.pause.action, s.pause.reason
		}
		if !s.guardUntil.IsZero() {
			st.PausedUntil = s.guardUntil.UnixMilli()
		}
		out = append(out, st)
	}
	return out
}

// NeedAction pauses a site until Reset, e.g. doubledouble before its first browser check. reason starts with
// the site's label, as a needs-action error's does.
// action is the settings action that lifts it, "" for none; a newer call replaces the reason and action.
func (b *Balancer) NeedAction(name, reason, action string) {
	b.mu.Lock()
	s := b.sites[name]
	if s == nil {
		b.mu.Unlock()
		return
	}
	s.pause = pause{needsAction: true, reason: reason, action: action}
	stopTimer(&s.coolTimer)
	b.mu.Unlock()
	b.changed()
}

// Reset lifts a cooldown or needed action, e.g. after the user passed the browser check. A guard pause stays.
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
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sites[name] != nil
}
