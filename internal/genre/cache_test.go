package genre

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCacheRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "genres.json")
	c := OpenCache(p)
	if _, ok := c.Get("x"); ok {
		t.Fatal("missing file should be empty cache")
	}
	c.Put("x", []string{"pop", "rap"})
	got, ok := OpenCache(p).Get("x")
	if !ok || !reflect.DeepEqual(got, []string{"pop", "rap"}) {
		t.Fatalf("got %q %v", got, ok)
	}
	if err := os.WriteFile(p, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := OpenCache(p).Get("x"); ok {
		t.Fatal("corrupt file should be empty cache")
	}
}
