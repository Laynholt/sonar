package history

import (
	"path/filepath"
	"testing"
)

func open(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAddReplacesSameTypeAndURL(t *testing.T) {
	db := open(t)
	if err := db.Add(FetchItem{Type: "album", URL: "u1", Name: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := db.Add(FetchItem{Type: "album", URL: "u1", Name: "new"}); err != nil {
		t.Fatal(err)
	}
	items, err := db.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "new" || items[0].ID != "album|u1" {
		t.Fatalf("got %+v", items)
	}
}

func TestClearByTypeLeavesOthers(t *testing.T) {
	db := open(t)
	for _, it := range []FetchItem{{Type: "album", URL: "a"}, {Type: "track", URL: "t"}, {Type: "playlist", URL: "p"}} {
		if err := db.Add(it); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.ClearByType("album"); err != nil {
		t.Fatal(err)
	}
	items, _ := db.List()
	if len(items) != 2 {
		t.Fatalf("got %+v", items)
	}
	for _, it := range items {
		if it.Type == "album" {
			t.Errorf("album survived: %+v", it)
		}
	}
}

func TestDeleteAndClear(t *testing.T) {
	db := open(t)
	db.Add(FetchItem{Type: "track", URL: "a"})
	db.Add(FetchItem{Type: "track", URL: "b"})
	if err := db.Delete("track|a"); err != nil {
		t.Fatal(err)
	}
	if items, _ := db.List(); len(items) != 1 || items[0].URL != "b" {
		t.Fatalf("after delete: %+v", items)
	}
	if err := db.Clear(); err != nil {
		t.Fatal(err)
	}
	if items, _ := db.List(); len(items) != 0 {
		t.Fatalf("after clear: %+v", items)
	}
	// Still usable after Clear.
	if err := db.Add(FetchItem{Type: "track", URL: "c"}); err != nil {
		t.Fatal(err)
	}
}
