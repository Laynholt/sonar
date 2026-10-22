package resolve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matches.json")
	c := OpenCache(path)
	c.Put("sp1", "A1", "title")
	c.Put("sp2", "A2", "manual")
	got := OpenCache(path)
	if id, via, ok := got.Get("sp2"); !ok || id != "A2" || via != "manual" {
		t.Fatalf("Get(sp2) = %q %q %v", id, via, ok)
	}
	if _, _, ok := got.Get("nope"); ok {
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
		if _, _, ok := c.Get("sp1"); ok {
			t.Fatal("corrupt cache returned a hit")
		}
		c.Put("sp1", "A1", "isrc") // must not panic and must repair the file
		if id, _, ok := OpenCache(path).Get("sp1"); !ok || id != "A1" {
			t.Fatalf("%q: rewrite failed", junk)
		}
	}
}
