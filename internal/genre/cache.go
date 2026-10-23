package genre

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// Cache maps Spotify track IDs to their genres, saved as JSON.
type Cache struct {
	mu   sync.Mutex
	path string
	m    map[string][]string
}

// OpenCache loads path; a missing or corrupt file gives an empty cache.
func OpenCache(path string) *Cache {
	var m map[string][]string
	raw, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(raw, &m)
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		log.Printf("genre cache %s: %v; starting empty", path, err)
		m = nil
	}
	if m == nil {
		m = map[string][]string{}
	}
	return &Cache{path: path, m: m}
}

func (c *Cache) Get(spotifyID string) ([]string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	g, ok := c.m[spotifyID]
	return slices.Clone(g), ok
}

// Put remembers genres and rewrites the file; a failed write is logged, the entry stays in memory.
func (c *Cache) Put(spotifyID string, genres []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[spotifyID] = slices.Clone(genres)
	if err := c.save(); err != nil {
		log.Printf("genre cache %s: %v", c.path, err)
	}
}

// save writes a temp file and renames it over the cache, so a crash never leaves half a file.
func (c *Cache) save() error {
	raw, err := json.Marshal(c.m)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), "genres-*.tmp")
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
