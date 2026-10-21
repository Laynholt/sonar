package amazon

import "testing"

func TestParseAmazonSearch(t *testing.T) {
	raw := []byte(`{"methods":[{"template":{"widgets":[
	 {"items":[{"primaryText":{"text":"Creep"},"secondaryText":"Radiohead","image":"i1","primaryLink":{"deeplink":"/albums/B01?trackAsin=T1"},"tags":["E"]}]},
	 {"items":[{"primaryText":"Pablo Honey","secondaryText":"Radiohead","primaryLink":{"deeplink":"/albums/B01"}},
	           {"primaryText":{"text":"Creep"},"primaryLink":{"deeplink":"/albums/B01?trackAsin=T1"}},
	           {"primaryText":{"text":"Radiohead"},"primaryLink":{"deeplink":"/artists/A1/radiohead"}}]}]}}]}`)
	items, err := parseAmazonSearch(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (dedup, no artists): %+v", len(items), items)
	}
	tr, al := items[0], items[1]
	if tr.Kind != "track" || tr.Title != "Creep" || !tr.Explicit || tr.URL != "https://music.amazon.com/albums/B01?trackAsin=T1" {
		t.Errorf("track: %+v", tr)
	}
	if al.Kind != "album" || al.Title != "Pablo Honey" || al.Artist != "Radiohead" {
		t.Errorf("album: %+v", al)
	}
	if _, err := parseAmazonSearch([]byte(`{"methods":[{"template":{"header":"Service error"}}]}`)); err == nil {
		t.Error("want error for Service error dialog")
	}
}
