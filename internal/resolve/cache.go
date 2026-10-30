package resolve

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type cached struct {
	TargetID string `json:"amazon_id"` // the name matches.json has always had
	Via      string `json:"via"`
}

// key is the map key of a track: its bare id for spotify to amazon, as matches.json always had it,
// else prefixed with the pair.
func key(from, to, id string) string {
	if from == "spotify" && to == "amazon" {
		return id
	}
	return from + ">" + to + ":" + id
}

// Cache maps track ids of one service to confirmed ids on another, saved as JSON.
type Cache struct {
	mu   sync.Mutex
	path string
	m    map[string]cached
}

// OpenCache loads path; a missing or corrupt file gives an empty cache.
func OpenCache(path string) *Cache {
	var m map[string]cached
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("match cache %s: %v; starting empty", path, err)
		m = nil
	}
	if m == nil {
		m = map[string]cached{}
	}
	return &Cache{path: path, m: m}
}

func (c *Cache) Get(from, to, id string) (targetID, via string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key(from, to, id)]
	return e.TargetID, e.Via, ok
}

// Put remembers a match and rewrites the file; a failed write is logged, the entry stays in memory.
func (c *Cache) Put(from, to, id, targetID, via string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key(from, to, id)] = cached{targetID, via}
	if err := c.save(); err != nil {
		log.Printf("match cache %s: %v", c.path, err)
	}
}

// save writes a temp file and renames it over the cache, so a crash never leaves half a file.
func (c *Cache) save() error {
	raw, err := json.Marshal(c.m)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), "matches-*.tmp")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), c.path)
	}
	if err != nil {
		os.Remove(f.Name())
	}
	return err
}
