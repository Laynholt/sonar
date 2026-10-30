package download

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Saved is one unfinished queue entry as written to queue.json.
type Saved struct {
	Item       Item       `json:"item"`
	Collection Collection `json:"collection"`
	Options    Options    `json:"options"`
}

// State is the content of queue.json.
type State struct {
	Version int              `json:"version"` // 1
	Items   []Saved          `json:"items"`
	Pauses  map[string]int64 `json:"pauses"` // site -> guard pause end, unix ms
}

// Pending returns the entries worth keeping across a restart: matching, queued, downloading (as queued), review and failed.
// Progress and site fields are cleared; IDs, Via, Candidates, ReviewService and Error are kept.
func (q *Queue) Pending() []Saved {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []Saved{} // "items": [] in the file, never null
	for _, e := range q.order {
		switch e.Status {
		case Matching, Queued, Downloading, Review, Failed:
		default:
			continue
		}
		it := e.Item
		it.Bytes, it.Size, it.Message, it.Site = 0, 0, "", ""
		if it.Status == Downloading {
			it.Status, it.Error = Queued, ""
		}
		out = append(out, Saved{Item: it, Collection: e.c, Options: e.o})
	}
	return out
}

// Restore appends saved entries whose IDs the queue does not know and holds the queue until Resume or Enqueue
// if any of them is not failed, as GetPendingQueue counts them. Review and failed entries keep their status;
// the rest start over as a new entry would.
func (q *Queue) Restore(s []Saved) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	hold, names := false, q.sites.Names()
	for _, sv := range s {
		if sv.Item.ID == "" || q.byID[sv.Item.ID] != nil {
			continue
		}
		e := &entry{Item: sv.Item, c: sv.Collection, o: sv.Options, pathNoExt: PathNoExt(sv.Item.Track, sv.Collection, sv.Options)}
		e.Bytes, e.Size, e.Message, e.Site = 0, 0, "", ""
		switch e.Status {
		case Review:
			if e.ReviewService == "" { // saved before review_service: reviews were for Amazon
				e.ReviewService = "amazon"
			}
		case Failed:
		case Matching, Queued, Downloading:
			e.Error = ""
			q.placeLocked(e, names, "")
		default: // the file is outside our control; anything else was finished
			continue
		}
		q.byID[e.ID] = e
		q.order = append(q.order, e)
		hold = hold || e.Status != Failed
	}
	if hold {
		q.releaseLocked()
		q.held = true
	}
}

// UnmarshalJSON reads an Item, taking a legacy "amazon_id" as IDs["amazon"] for a track from another service.
func (it *Item) UnmarshalJSON(b []byte) error {
	type plain Item // without this method
	var v struct {
		plain
		AmazonID string `json:"amazon_id"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*it = Item(v.plain)
	if v.AmazonID != "" && ServiceOf(it.Track) != "amazon" && it.IDs["amazon"] == "" {
		it.IDs = withID(it.IDs, "amazon", v.AmazonID)
	}
	return nil
}

// Clear cancels running downloads and matches and drops every entry except completed and skipped ones.
// The hold is left as it is.
func (q *Queue) Clear() {
	defer q.notify()
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.order[:0]
	for _, e := range q.order {
		if e.Status == Completed || e.Status == Skipped {
			kept = append(kept, e)
			continue
		}
		e.stop() // a dropped entry's late match or download result lands on the detached entry
		delete(q.byID, e.ID)
	}
	clear(q.order[len(kept):])
	q.order = kept
	q.cond.Broadcast()
}

func (q *Queue) Held() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.held
}

// SaveState writes s as indented JSON through a temp file renamed over path, so a crash never leaves half a file.
func SaveState(path string, s State) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "queue-*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync() // the rename must not land before the data does
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}

// LoadState reads path; a missing file is an empty version-1 state, a corrupt one an error.
func LoadState(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{Version: 1}, nil
	}
	if err != nil {
		return State{}, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, err
	}
	return s, nil
}
