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
	AmazonID string `json:"amazon_id"`
	Via      string `json:"via"`
}

// Cache maps Spotify track IDs to confirmed Amazon IDs, saved as JSON.
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

func (c *Cache) Get(spotifyID string) (amazonID, via string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[spotifyID]
	return e.AmazonID, e.Via, ok
}

// Put remembers a match and rewrites the file; a failed write is logged, the entry stays in memory.
func (c *Cache) Put(spotifyID, amazonID, via string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[spotifyID] = cached{amazonID, via}
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
