package download

import (
	"context"
	"errors"
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
	ID         string      `json:"id"`
	QueueID    string      `json:"queue_id"`
	Track      Track       `json:"track"`
	Status     Status      `json:"status"`
	Message    string      `json:"message"`
	Path       string      `json:"path"`
	Error      string      `json:"error"`
	Bytes      int64       `json:"bytes"` // written so far
	Size       int64       `json:"size"`  // expected file size, 0 when the site does not say
	AmazonID   string      `json:"amazon_id"`
	Via        string      `json:"via"`
	Candidates []Candidate `json:"candidates,omitempty"`
	Site       string      `json:"site"`
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

type Resolution struct {
	AmazonID   string
	Via        string
	Candidates []Candidate
	Err        error
}

// Resolve returns one Resolution per track, same order. AmazonID != "" means confident.
// It must return promptly once ctx is done: Close waits for it.
type Resolve func(ctx context.Context, tracks []Track) []Resolution

// Sites are the download sites, one worker each. Ready and Usable are called under the queue lock,
// so they must not call back into the Queue.
type Sites interface {
	Names() []string
	Ready(name string) bool
	// Usable is false while only the user can make the site ready again, e.g. by passing a browser check.
	Usable(name string) bool
}

// ErrSiteDown marks a Fetch error that is the site's fault, not the track's: in auto mode the track waits for another site.
// Sites must stop reporting that site Ready before Fetch returns it, or the site retakes the track at once.
var ErrSiteDown = errors.New("download site unavailable")

// Fetch downloads one track through site to pathNoExt+extension and returns the file path.
// t is the entry's Track and amazonID its AmazonID. onStatus must not be called after Fetch returns.
type Fetch func(ctx context.Context, site string, t Track, amazonID string, o Options, pathNoExt string, onStatus func(string)) (string, error)

type entry struct {
	Item
	c         Collection
	o         Options
	pathNoExt string
	cancel    context.CancelFunc // set while Downloading
	attempt   int                // bumped on each entry into Matching; a result for an older attempt is dropped
	tried     []string           // auto mode: sites that failed the track, in order; cleared by Retry and Enqueue
	errs      []string           // "site: err" for each of tried
}

// Queue matches tracks one job at a time and downloads them one track per site;
// one entry per track ID, in first-enqueue order.
type Queue struct {
	fetch      Fetch
	resolve    Resolve
	sites      Sites
	names      []string
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

// NewQueue starts one worker per site and the matcher. A nil resolve fails every Spotify track with "no match result".
func NewQueue(fetch Fetch, resolve Resolve, sites Sites) *Queue {
	names := sites.Names()
	q := &Queue{fetch: fetch, resolve: resolve, sites: sites, names: names, byID: map[string]*entry{}, busy: map[string]bool{}}
	q.base, q.cancelBase = context.WithCancel(context.Background())
	q.cond = sync.NewCond(&q.mu)
	q.wg.Add(len(names) + 1)
	for _, name := range names {
		go q.worker(name)
	}
	go q.matcher()
	return q
}

// Wake makes idle workers look again, e.g. after a site became ready.
func (q *Queue) Wake() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cond.Broadcast()
}

// Enqueue adds tracks and releases a hold. A track already matching, in review, queued or downloading is left as it is.
func (q *Queue) Enqueue(c Collection, o Options, tracks []Track) {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
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
		if t.Service == "" || t.Service == "amazon" {
			e.AmazonID = t.ID
		}
		e.begin()
	}
	q.releaseLocked()
	q.cond.Broadcast()
}

// begin starts a fresh or retried entry: matching until it has an Amazon ID, then queued.
func (e *entry) begin() {
	e.Status = Queued
	if e.AmazonID == "" {
		e.Status = Matching
		e.attempt++
	}
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

// Choose downloads amazonID for an item in review.
func (q *Queue) Choose(id, amazonID string) error {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	e := q.byID[id]
	if e == nil || e.Status != Review {
		return errors.New("track is not waiting for a choice")
	}
	if amazonID == "" {
		return errors.New("no Amazon track chosen")
	}
	e.Status, e.AmazonID, e.Via = Queued, amazonID, "manual"
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
		e.Status = Cancelled
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
		q.requeue(e)
	}
}

func (q *Queue) RetryFailed() {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range q.order {
		if e.Status == Failed {
			q.requeue(e)
		}
	}
}

// requeue sends a failed or cancelled item back to matching when it never got an Amazon ID, else to the download queue.
func (q *Queue) requeue(e *entry) {
	if q.closed || (e.Status != Failed && e.Status != Cancelled) {
		return
	}
	e.Error, e.Message, e.Path, e.Bytes, e.Size = "", "", "", 0, 0
	e.tried, e.errs = nil, nil
	if e.begin(); e.Status == Matching {
		e.Candidates = nil
	}
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

// matcher resolves one job at a time: the oldest matching entry with the rest of its album group.
func (q *Queue) matcher() {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		var job []*entry
		if !q.waitLocked(func() bool { job = q.matchJobLocked(); return job != nil }) {
			q.mu.Unlock()
			return
		}
		tracks, attempts := make([]Track, len(job)), make([]int, len(job))
		for i, e := range job {
			tracks[i], attempts[i] = e.Track, e.attempt
		}
		q.mu.Unlock()

		var res []Resolution
		if q.resolve != nil {
			ctx, cancel := context.WithTimeout(q.base, matchTimeout*time.Duration(len(job)))
			res = q.resolve(ctx, tracks)
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
			e.Candidates = r.Candidates
			switch {
			case r.Err != nil:
				e.Status, e.Error = Failed, r.Err.Error()
			case r.AmazonID != "":
				e.Status, e.AmazonID, e.Via = Queued, r.AmazonID, r.Via
			default:
				e.Status = Review
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
		if e.Status == Matching && (job == nil || job[0].Track.Group != "" && e.Track.Group == job[0].Track.Group) {
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
		id, track := e.AmazonID, e.Track
		q.mu.Unlock()
		q.notify()
		ctx = WithProgress(ctx, func(done, total int64) {
			q.mu.Lock()
			defer q.mu.Unlock()
			if e.Status == Downloading {
				e.Bytes, e.Size = done, total
			}
		})

		path, err := q.fetch(ctx, site, track, id, e.o, e.pathNoExt, func(msg string) {
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
		case !e.auto():
			e.Status, e.Error = Failed, err.Error()
		default:
			e.Status, e.Message, e.Bytes, e.Size = Queued, "", 0, 0
			errs := append(slices.Clip(e.errs), err.Error())
			if !errors.Is(err, ErrSiteDown) { // a site down is not tried: its gate keeps it off this track until it is back
				msg := err.Error()
				if !strings.HasPrefix(msg, site+": ") { // the balancer usually prefixes it already
					msg = site + ": " + msg
				}
				e.tried, e.errs = append(e.tried, site), append(e.errs, msg)
				errs = e.errs
			}
			if !q.usableLocked(e) {
				e.Status, e.Error = Failed, strings.Join(errs, "; ")
			}
		}
		q.cond.Broadcast() // the track may be back in the queue, and its path is free
		q.mu.Unlock()
		q.notify()
	}
}

// usableLocked reports whether a site that has not tried e may still take it without the user's help.
func (q *Queue) usableLocked(e *entry) bool {
	return slices.ContainsFunc(q.names, func(n string) bool { return !slices.Contains(e.tried, n) && q.sites.Usable(n) })
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
		if q.busy[e.pathNoExt] {
			continue // same target as a running download; wait so they don't share a .part
		}
		return e
	}
	return nil
}
