package download

import (
	"context"
	"sync"
	"time"
)

type Status string

const (
	Queued      Status = "queued"
	Downloading Status = "downloading"
	Completed   Status = "completed"
	Skipped     Status = "skipped"
	Failed      Status = "failed"
	Cancelled   Status = "cancelled"
)

// trackTimeout bounds one track; the HTTP clients have no total timeout of their own.
const trackTimeout = 15 * time.Minute

type Item struct {
	ID      string `json:"id"`
	QueueID string `json:"queue_id"`
	Track   Track  `json:"track"`
	Status  Status `json:"status"`
	Message string `json:"message"`
	Path    string `json:"path"`
	Error   string `json:"error"`
}

// Fetch downloads one track to pathNoExt+extension and returns the file path.
// onStatus must not be called after Fetch returns.
type Fetch func(ctx context.Context, trackID string, o Options, pathNoExt string, onStatus func(string)) (string, error)

type entry struct {
	Item
	o         Options
	pathNoExt string
	cancel    context.CancelFunc // set while Downloading
}

// Queue runs downloads on a fixed set of workers; one entry per track ID, in first-enqueue order.
type Queue struct {
	fetch  Fetch
	mu     sync.Mutex
	cond   *sync.Cond
	order  []*entry
	byID   map[string]*entry
	closed bool
	wg     sync.WaitGroup

	held      bool        // workers start nothing while held
	heldUntil time.Time   // zero = until Resume
	holdTimer *time.Timer // auto-resume for a timed hold
}

func NewQueue(fetch Fetch, workers int) *Queue {
	q := &Queue{fetch: fetch, byID: map[string]*entry{}}
	q.cond = sync.NewCond(&q.mu)
	for i := 0; i < workers; i++ {
		q.wg.Add(1)
		go q.worker()
	}
	return q
}

func (q *Queue) Enqueue(c Collection, o Options, tracks []Track) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	for _, t := range tracks {
		e := q.byID[t.ID]
		if e != nil && (e.Status == Queued || e.Status == Downloading) {
			continue
		}
		if e == nil {
			e = &entry{}
			q.byID[t.ID] = e
			q.order = append(q.order, e)
		}
		*e = entry{Item: Item{ID: t.ID, QueueID: c.Kind + ":" + c.ID, Track: t, Status: Queued}, o: o, pathNoExt: PathNoExt(t, c, o)}
	}
	q.cond.Broadcast()
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

func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if e := q.byID[id]; e != nil {
		e.stop()
	}
}

func (q *Queue) CancelAll() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancelAllLocked()
}

func (q *Queue) cancelAllLocked() {
	for _, e := range q.order {
		e.stop()
	}
}

// stop cancels a queued item at once and a downloading one through its context; finished items are left alone.
func (e *entry) stop() {
	switch e.Status {
	case Queued:
		e.Status = Cancelled
	case Downloading:
		e.cancel()
	}
}

// Retry re-queues a failed or cancelled item with its stored options and path.
func (q *Queue) Retry(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if e := q.byID[id]; e != nil {
		q.requeue(e)
	}
}

func (q *Queue) RetryFailed() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, e := range q.order {
		if e.Status == Failed {
			q.requeue(e)
		}
	}
}

func (q *Queue) requeue(e *entry) {
	if q.closed || (e.Status != Failed && e.Status != Cancelled) {
		return
	}
	e.Status, e.Error, e.Message, e.Path = Queued, "", "", ""
	q.cond.Broadcast()
}

// ClearFinished drops everything that is neither queued nor downloading.
func (q *Queue) ClearFinished() {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.order[:0]
	for _, e := range q.order {
		if e.Status == Queued || e.Status == Downloading {
			kept = append(kept, e)
		} else {
			delete(q.byID, e.ID)
		}
	}
	clear(q.order[len(kept):])
	q.order = kept
}

// Hold stops workers from starting queued items; in-flight ones finish. A zero until holds until Resume.
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

// Close cancels everything and waits for the workers.
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.releaseLocked() // stops a pending auto-resume timer
	q.cancelAllLocked()
	q.cond.Broadcast()
	q.mu.Unlock()
	q.wg.Wait()
}

func (q *Queue) worker() {
	defer q.wg.Done()
	for {
		q.mu.Lock()
		var e *entry
		for {
			if q.closed {
				break
			}
			if !q.held {
				if e = q.takeLocked(); e != nil {
					break
				}
			}
			q.cond.Wait()
		}
		if e == nil {
			q.mu.Unlock()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), trackTimeout)
		e.Status, e.cancel = Downloading, cancel
		q.mu.Unlock()

		path, err := q.fetch(ctx, e.ID, e.o, e.pathNoExt, func(msg string) {
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
		switch {
		case err == nil:
			e.Status, e.Path = Completed, path
		case canceled:
			e.Status = Cancelled
		default:
			e.Status, e.Error = Failed, err.Error()
		}
		q.mu.Unlock()
	}
}

// takeLocked returns the oldest queued item that still needs a download; items already on disk become Skipped on the way.
func (q *Queue) takeLocked() *entry {
	for _, e := range q.order {
		if e.Status != Queued {
			continue
		}
		if p := Existing(e.pathNoExt, e.o.Format); p != "" {
			e.Status, e.Path = Skipped, p
			continue
		}
		if q.busyPathLocked(e) {
			continue // same target as a running download; wait so they don't share a .part
		}
		return e
	}
	return nil
}

func (q *Queue) busyPathLocked(e *entry) bool {
	for _, o := range q.order {
		if o != e && o.Status == Downloading && o.pathNoExt == e.pathNoExt {
			return true
		}
	}
	return false
}
