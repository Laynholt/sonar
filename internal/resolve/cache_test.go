package resolve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matches.json")
	c := OpenCache(path)
	c.Put("spotify", "amazon", "sp1", "A1", "title", "")
	c.Put("spotify", "amazon", "sp2", "A2", "manual", "")
	got := OpenCache(path)
	if id, via, _, ok := got.Get("spotify", "amazon", "sp2"); !ok || id != "A2" || via != "manual" {
		t.Fatalf("Get(sp2) = %q %q %v", id, via, ok)
	}
	if _, _, _, ok := got.Get("spotify", "amazon", "nope"); ok {
		t.Error("Get(nope) found something")
	}
}

func TestCacheCorruptFileIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matches.json")
	for _, junk := range []string{"{not json", "null"} {
		if err := os.WriteFile(path, []byte(junk), 0o644); err != nil {
			t.Fatal(err)
		}
		c := OpenCache(path)
		if _, _, _, ok := c.Get("spotify", "amazon", "sp1"); ok {
			t.Fatal("corrupt cache returned a hit")
		}
		c.Put("spotify", "amazon", "sp1", "A1", "isrc", "") // must not panic and must repair the file
		if id, _, _, ok := OpenCache(path).Get("spotify", "amazon", "sp1"); !ok || id != "A1" {
			t.Fatalf("%q: rewrite failed", junk)
		}
	}
}

func TestCacheLegacyKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matches.json")
	if err := os.WriteFile(path, []byte(`{"sp1":{"amazon_id":"A1","via":"title"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := OpenCache(path)
	if id, via, _, ok := c.Get("spotify", "amazon", "sp1"); !ok || id != "A1" || via != "title" {
		t.Fatalf("legacy Get = %q %q %v", id, via, ok)
	}
	if _, _, _, ok := c.Get("deezer", "amazon", "sp1"); ok {
		t.Error("a legacy entry answers another origin")
	}
	c.Put("deezer", "tidal", "d1", "T1", "isrc", "")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]cached
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if m["deezer>tidal:d1"].TargetID != "T1" || m["sp1"].TargetID != "A1" || len(m) != 2 {
		t.Errorf("file = %s", raw)
	}
}
