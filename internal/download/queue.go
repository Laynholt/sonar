package download

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	Matching    Status = "matching" // waiting for or in a Resolve call
	Review      Status = "review"   // no confident match; the user chooses or skips
	Queued      Status = "queued"
	Downloading Status = "downloading"
	Completed   Status = "completed"
	Skipped     Status = "skipped"
	Ignored     Status = "ignored"   // skipped in review with candidates on offer
	NotFound    Status = "not_found" // skipped in review with no candidates
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
)

// trackTimeout bounds one track; the HTTP clients have no total timeout of their own.
const trackTimeout = 15 * time.Minute

// matchTimeout bounds one Resolve call per track in its job: a group may fall back to matching track by track.
const matchTimeout = 5 * time.Minute

type Item struct {
	ID         string            `json:"id"`
	QueueID    string            `json:"queue_id"`
	Track      Track             `json:"track"`
	Status     Status            `json:"status"`
	Message    string            `json:"message"`
	Path       string            `json:"path"`
	Error      string            `json:"error"`
	Bytes      int64             `json:"bytes"`         // written so far
	Size       int64             `json:"size"`          // expected file size, 0 when the site does not say
	IDs        map[string]string `json:"ids,omitempty"` // service → id found for this track (its own service's id is not stored here)
	Via        string            `json:"via"`
	Candidates []Candidate       `json:"candidates,omitempty"`
	// ReviewService is the service the candidates of an item in review belong to.
	ReviewService string `json:"review_service,omitempty"`
	Site          string `json:"site"`
}

// ServiceOf is the service t belongs to: t.Service, "" meaning amazon.
func ServiceOf(t Track) string {
	if t.Service == "" {
		return "amazon"
	}
	return t.Service
}

// withID returns a copy of ids with service set to id. IDs maps are never written in place:
// a Snapshot or a running Fetch may hold the old one.
func withID(ids map[string]string, service, id string) map[string]string {
	out := maps.Clone(ids)
	if out == nil {
		out = map[string]string{}
	}
	out[service] = id
	return out
}

type Candidate struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Artist      string  `json:"artist"`
	Album       string  `json:"album"`
	ReleaseDate string  `json:"release_date"`
	Image       string  `json:"image"`
	DurationMS  int     `json:"duration_ms"`
	Explicit    bool    `json:"explicit"`
	Score       float64 `json:"score"`
	Recommended bool    `json:"recommended"`
}

// Resolution is one track's match on another service. ISRC, when set, fills the track's ISRC.
type Resolution struct {
	ID, Via, ISRC string
	Candidates    []Candidate
	Err           error
}

// Resolve matches tracks to the service to and returns one Resolution per track, same order. ID != "" means confident.
// It must return promptly once ctx is done: Close waits for it.
type Resolve func(ctx context.Context, to string, tracks []Track) []Resolution

// Sites are the download sites. All of its methods are called under the queue lock,
// so they must not call back into the Queue.
type Sites interface {
	Names() []string
	Ready(name string) bool
	// Usable is false while only the user can make the site ready again, e.g. by passing a browser check.
	Usable(name string) bool
	// Accepts is the service whose ids the site downloads, or "any" for a site that takes every track as it is.
	Accepts(name string) string
}

// ErrSiteDown marks a Fetch error that is the site's fault, not the track's: in auto mode the track waits for another site.
// Sites must stop reporting that site Ready before Fetch returns it, or the site retakes the track at once.
var ErrSiteDown = errors.New("download site unavailable")

// ErrSiteOff marks a Fetch cut short because the user switched its site off. It wraps ErrSiteDown, but never fails
// the track nor counts as a try, even for a fixed source: the track is placed again, and may wait Queued.
var ErrSiteOff = fmt.Errorf("site switched off: %w", ErrSiteDown)

// Fetch downloads one track through site to pathNoExt+extension and returns the file path.
// t is the entry's Track and ids its IDs, which Fetch must not modify. onStatus must not be called after Fetch returns.
type Fetch func(ctx context.Context, site string, t Track, ids map[string]string, o Options, pathNoExt string, onStatus func(string)) (string, error)

type entry struct {
	Item
	c         Collection
	o         Options
	pathNoExt string
	cancel    context.CancelFunc // set while Downloading
	attempt   int                // bumped on each entry into Matching; a result for an older attempt is dropped
	target    string             // while Matching: the service to match to
	tried     []string           // auto mode: sites that failed the track, in order; cleared by Retry and Enqueue
	errs      []string           // "site: err" for each of tried
}

// Queue matches tracks one job at a time and downloads them through each site's workers;
// one entry per track ID, in first-enqueue order.
type Queue struct {
	fetch      Fetch
	resolve    Resolve
	sites      Sites
	started    map[string]bool // sites with workers
	base       context.Context // parent of every Resolve call; cancelled by Close
	cancelBase context.CancelFunc
	mu         sync.Mutex
	cond       *sync.Cond
	order      []*entry
	byID       map[string]*entry
	closed     bool
	wg         sync.WaitGroup

	held      bool        // workers and the matcher start nothing while held
	heldUntil time.Time   // zero = until Resume
	holdTimer *time.Timer // auto-resume for a timed hold

	busy map[string]bool // pathNoExt of each running download, kept after Clear drops its entry

	// OnChange, if set before the first Enqueue or Restore, is called outside the lock after a status change,
	// Enqueue, Choose, Skip, Cancel, Retry, Clear or ClearFinished, never for progress and never after Close.
	// It may call back into the Queue but should return quickly.
	OnChange func()
}

// NewQueue starts one worker per site and the matcher. A nil resolve fails every track that needs matching with "no match result".
func NewQueue(fetch Fetch, resolve Resolve, sites Sites) *Queue {
	q := &Queue{fetch: fetch, resolve: resolve, sites: sites, started: map[string]bool{}, byID: map[string]*entry{}, busy: map[string]bool{}}
	q.base, q.cancelBase = context.WithCancel(context.Background())
	q.cond = sync.NewCond(&q.mu)
	q.wg.Add(1)
	go q.matcher()
	for _, name := range sites.Names() {
		q.AddSite(name, 1)
	}
	return q
}

// AddSite starts workers for name, which Sites.Names must list, and sends queued tracks that no site
// can take as they are to matching. A second call for the same name, or one after Close, does nothing.
func (q *Queue) AddSite(name string, workers int) {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || q.started[name] {
		return
	}
	q.started[name] = true
	workers = max(workers, 1) // a started site without a worker would hold its tracks forever
	q.wg.Add(workers)
	for range workers {
		go q.worker(name)
	}
	q.replaceLocked()
	q.cond.Broadcast()
}

// Wake makes idle workers look again, e.g. after a site became ready, and sends queued tracks that no usable
// site can take as they are to matching, e.g. after the only site that could became unusable.
// It calls OnChange only when a track changed, and Sites under the lock, as everywhere else.
func (q *Queue) Wake() {
	q.mu.Lock()
	changed := q.replaceLocked()
	q.cond.Broadcast()
	q.mu.Unlock()
	if changed {
		q.notify()
	}
}

// replaceLocked places every queued entry again and reports whether any of them went to matching.
func (q *Queue) replaceLocked() bool {
	names, changed := q.sites.Names(), false
	for _, e := range q.order {
		if e.Status == Queued {
			q.placeLocked(e, names, "")
			changed = changed || e.Status != Queued
		}
	}
	return changed
}

// Enqueue adds tracks and releases a hold. A track already matching, in review, queued or downloading is left as it is.
func (q *Queue) Enqueue(c Collection, o Options, tracks []Track) {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	names := q.sites.Names()
	for _, t := range tracks {
		e := q.byID[t.ID]
		if e != nil && (e.Status == Matching || e.Status == Review || e.Status == Queued || e.Status == Downloading) {
			continue
		}
		if e == nil {
			e = &entry{}
			q.byID[t.ID] = e
			q.order = append(q.order, e)
		}
		*e = entry{Item: Item{ID: t.ID, QueueID: c.Kind + ":" + c.ID, Track: t}, c: c, o: o, pathNoExt: PathNoExt(t, c, o), attempt: e.attempt}
		q.placeLocked(e, names, "")
	}
	q.releaseLocked()
	q.cond.Broadcast()
}

// placeLocked queues e when a site it may go to can take it as it is; otherwise it sends e to matching for
// the service of the first of those sites. down, a site that has just gone down on e, is left out.
// names is Sites.Names, fetched once by the caller.
// In auto mode with no site it may go to, e.g. while every site waits for a check, it matches e for the first
// started site whose service e has no id for, so e is ready once a site is back; with none it waits queued.
func (q *Queue) placeLocked(e *entry, names []string, down string) {
	e.Status, e.target = Queued, ""
	target := ""
	cands := q.candidatesLocked(e, names)
	for _, n := range cands {
		if n == down {
			continue
		}
		if q.canTake(e, n) {
			return
		}
		if target == "" {
			target = q.sites.Accepts(n)
		}
	}
	if len(cands) == 0 && e.auto() {
		for _, n := range names {
			if a := q.sites.Accepts(n); q.started[n] && a != "" && a != "any" && a != ServiceOf(e.Track) && e.IDs[a] == "" {
				target = a
				break
			}
		}
	}
	if target != "" {
		e.Status, e.target, e.Candidates = Matching, target, nil
		e.attempt++
	}
}

// candidatesLocked lists the sites e may go to: in auto mode the started, usable sites that have not tried it,
// in Names order; otherwise o.Source once started.
func (q *Queue) candidatesLocked(e *entry, names []string) []string {
	if !e.auto() {
		if q.started[e.o.Source] {
			return []string{e.o.Source}
		}
		return nil
	}
	var out []string
	for _, n := range names {
		if q.started[n] && !slices.Contains(e.tried, n) && q.sites.Usable(n) {
			out = append(out, n)
		}
	}
	return out
}

// canTake reports whether site can download e with what e has: its own service's id or one found for it.
func (q *Queue) canTake(e *entry, site string) bool {
	a := q.sites.Accepts(site)
	return a == "any" || a == ServiceOf(e.Track) || e.IDs[a] != ""
}

func (q *Queue) Snapshot() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, 0, len(q.order))
	for _, e := range q.order {
		out = append(out, e.Item)
	}
	return out
}

// Choose downloads targetID, an id on the item's ReviewService, for an item in review.
func (q *Queue) Choose(id, targetID string) error {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.byID[id]
	if e == nil || e.Status != Review {
		return errors.New("track is not waiting for a choice")
	}
	if targetID == "" {
		return errors.New("no track chosen")
	}
	e.Status, e.IDs, e.Via = Queued, withID(e.IDs, e.ReviewService, targetID), "manual"
	q.cond.Broadcast()
	return nil
}

// Skip gives up on an item in review: not_found when nothing was offered, ignored otherwise.
func (q *Queue) Skip(id string) {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	if e := q.byID[id]; e != nil && e.Status == Review {
		e.Status = Ignored
		if len(e.Candidates) == 0 {
			e.Status = NotFound
		}
	}
}

func (q *Queue) Cancel(id string) {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	if e := q.byID[id]; e != nil {
		e.stop()
	}
}

func (q *Queue) CancelAll() {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancelAllLocked()
}

func (q *Queue) cancelAllLocked() {
	for _, e := range q.order {
		e.stop()
	}
}

// stop cancels a waiting item at once and a downloading one through its context; finished items are left alone.
// A Resolve call already running for a matching item is not interrupted; its result is dropped.
func (e *entry) stop() {
	switch e.Status {
	case Matching, Review, Queued:
		e.Status, e.target = Cancelled, ""
	case Downloading:
		e.cancel()
	}
}

// Retry re-queues a failed or cancelled item with its stored options and path.
func (q *Queue) Retry(id string) {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	if e := q.byID[id]; e != nil {
		q.requeue(e, q.sites.Names())
	}
}

func (q *Queue) RetryFailed() {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	names := q.sites.Names()
	for _, e := range q.order {
		if e.Status == Failed {
			q.requeue(e, names)
		}
	}
}

// requeue sends a failed or cancelled item back to the download queue, or to matching when no site can take it.
func (q *Queue) requeue(e *entry, names []string) {
	if q.closed || (e.Status != Failed && e.Status != Cancelled) {
		return
	}
	e.Error, e.Message, e.Path, e.Bytes, e.Size = "", "", "", 0, 0
	e.tried, e.errs = nil, nil
	q.placeLocked(e, names, "")
	q.cond.Broadcast()
}

// ClearFinished drops everything that is not matching, in review, queued or downloading.
func (q *Queue) ClearFinished() {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.order[:0]
	for _, e := range q.order {
		switch e.Status {
		case Matching, Review, Queued, Downloading:
			kept = append(kept, e)
		default:
			delete(q.byID, e.ID)
		}
	}
	clear(q.order[len(kept):])
	q.order = kept
}

// Hold stops workers and the matcher from starting anything; in-flight ones finish. A zero until holds until Resume.
func (q *Queue) Hold(until time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.releaseLocked()
	q.held, q.heldUntil = true, until
	if until.IsZero() {
		return
	}
	var t *time.Timer
	t = time.AfterFunc(time.Until(until), func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		if q.holdTimer == t { // a newer Hold or Resume replaced this one
			q.releaseLocked()
			q.cond.Broadcast()
		}
	})
	q.holdTimer = t
}

func (q *Queue) Resume() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.releaseLocked()
	q.cond.Broadcast()
}

func (q *Queue) HeldUntil() (held bool, until time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.held, q.heldUntil
}

func (q *Queue) releaseLocked() {
	if q.holdTimer != nil {
		q.holdTimer.Stop()
		q.holdTimer = nil
	}
	q.held, q.heldUntil = false, time.Time{}
}

// notify calls OnChange unless the queue is closed: Close's cancellations must not overwrite the save made before it.
// Callers must not hold q.mu.
func (q *Queue) notify() {
	q.mu.Lock()
	closed := q.closed
	q.mu.Unlock()
	if !closed && q.OnChange != nil {
		q.OnChange()
	}
}

// Close cancels everything and waits for the workers and the matcher.
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.releaseLocked() // stops a pending auto-resume timer
	q.cancelAllLocked()
	q.cancelBase()
	q.cond.Broadcast()
	q.mu.Unlock()
	q.wg.Wait()
}

// waitLocked blocks until take succeeds while the queue is open and not held; false once closed.
func (q *Queue) waitLocked(take func() bool) bool {
	for !q.closed {
		if !q.held && take() {
			return true
		}
		q.cond.Wait()
	}
	return false
}

// matcher resolves one job at a time: the oldest matching entry with the rest of its album group
// that matches to the same service.
func (q *Queue) matcher() {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		var job []*entry
		if !q.waitLocked(func() bool { job = q.matchJobLocked(); return job != nil }) {
			q.mu.Unlock()
			return
		}
		tracks, attempts, to := make([]Track, len(job)), make([]int, len(job)), job[0].target
		for i, e := range job {
			tracks[i], attempts[i] = e.Track, e.attempt
		}
		q.mu.Unlock()

		var res []Resolution
		if q.resolve != nil {
			ctx, cancel := context.WithTimeout(q.base, matchTimeout*time.Duration(len(job)))
			res = q.resolve(ctx, to, tracks)
			cancel()
		}

		q.mu.Lock()
		for i, e := range job {
			if e.Status != Matching || e.attempt != attempts[i] { // cancelled, or retried or re-enqueued meanwhile
				continue
			}
			r := Resolution{Err: errors.New("no match result")}
			if i < len(res) {
				r = res[i]
			}
			e.Candidates, e.target = r.Candidates, ""
			if r.ISRC != "" {
				e.Track.ISRC = r.ISRC
			}
			switch {
			case r.Err != nil: // after a fallback, the sites' errors come first
				e.Status, e.Error = Failed, strings.Join(append(slices.Clip(e.errs), r.Err.Error()), "; ")
			case r.ID != "":
				e.Status, e.IDs, e.Via = Queued, withID(e.IDs, to, r.ID), r.Via
			default:
				e.Status, e.ReviewService = Review, to
			}
		}
		q.cond.Broadcast()
		q.mu.Unlock()
		q.notify()
	}
}

func (q *Queue) matchJobLocked() []*entry {
	var job []*entry
	for _, e := range q.order {
		if e.Status == Matching && (job == nil || job[0].Track.Group != "" && e.Track.Group == job[0].Track.Group && e.target == job[0].target) {
			job = append(job, e)
		}
	}
	return job
}

// worker downloads through one site, one track at a time.
func (q *Queue) worker(site string) {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		var e *entry
		if !q.waitLocked(func() bool { e = q.takeLocked(site); return e != nil }) {
			q.mu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), trackTimeout)
		e.Status, e.cancel, e.Site = Downloading, cancel, site
		q.busy[e.pathNoExt] = true
		ids, track := e.IDs, e.Track
		q.mu.Unlock()
		q.notify()
		ctx = WithProgress(ctx, func(done, total int64) {
			q.mu.Lock()
			defer q.mu.Unlock()
			if e.Status == Downloading {
				e.Bytes, e.Size = done, total
			}
		})

		path, err := q.fetch(ctx, site, track, ids, e.o, e.pathNoExt, func(msg string) {
			q.mu.Lock()
			defer q.mu.Unlock()
			if e.Status == Downloading {
				e.Message = msg
			}
		})
		canceled := ctx.Err() == context.Canceled
		cancel()

		q.mu.Lock()
		e.cancel = nil
		delete(q.busy, e.pathNoExt)
		switch {
		case err == nil:
			e.Status, e.Path = Completed, path
		case canceled:
			e.Status = Cancelled
		case errors.Is(err, ErrSiteOff): // the user's switch, not the track's fault: no try, no failure
			e.Message, e.Bytes, e.Size = "", 0, 0
			q.placeLocked(e, q.sites.Names(), site)
		case !e.auto():
			e.Status, e.Error = Failed, err.Error()
		default:
			e.Message, e.Bytes, e.Size = "", 0, 0
			errs := append(slices.Clip(e.errs), err.Error())
			down := site // a site down is not tried: its gate keeps it off this track until it is back
			if !errors.Is(err, ErrSiteDown) {
				msg := err.Error()
				if !strings.HasPrefix(msg, site+": ") { // the balancer usually prefixes it already
					msg = site + ": " + msg
				}
				e.tried, e.errs = append(e.tried, site), append(e.errs, msg)
				errs, down = e.errs, ""
			}
			names := q.sites.Names()
			if len(q.candidatesLocked(e, names)) == 0 { // no site left that may take it without the user's help
				e.Status, e.Error = Failed, strings.Join(errs, "; ")
			} else {
				q.placeLocked(e, names, down) // to matching when no site left can take it as it is
			}
		}
		q.cond.Broadcast() // the track may be back in the queue, and its path is free
		q.mu.Unlock()
		q.notify()
	}
}

// auto reports whether any site may take the track; otherwise only o.Source may.
func (e *entry) auto() bool { return e.o.Source == "" || e.o.Source == "auto" }

// takeLocked returns the oldest queued item site may take now; items already on disk become Skipped on the way.
func (q *Queue) takeLocked(site string) *entry {
	if !q.sites.Ready(site) {
		return nil
	}
	for _, e := range q.order {
		if e.Status != Queued {
			continue
		}
		if p := Existing(e.pathNoExt, e.o.Format); p != "" {
			e.Status, e.Path = Skipped, p
			continue
		}
		if e.auto() {
			if slices.Contains(e.tried, site) {
				continue
			}
		} else if e.o.Source != site {
			continue
		}
		if !q.canTake(e, site) {
			continue
		}
		if q.busy[e.pathNoExt] {
			continue // same target as a running download; wait so they don't share a .part
		}
		return e
	}
	return nil
}
