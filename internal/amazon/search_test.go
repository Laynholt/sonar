package amazon

import (
	"os"
	"strings"
	"testing"
)

func TestParseSearchResponse(t *testing.T) {
	raw, err := os.ReadFile("testdata/search.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := parseSearchResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tracks) != 2 || len(r.Albums) != 2 || len(r.Artists) != 2 || len(r.Playlists) != 2 {
		t.Fatalf("counts: %d tracks, %d albums, %d artists, %d playlists", len(r.Tracks), len(r.Albums), len(r.Artists), len(r.Playlists))
	}
	tr := r.Tracks[0]
	if tr.ID != "B0HMY12BH5" || tr.Name != "expectations" || tr.Type != "track" ||
		tr.Artists != "Olivia Rodrigo & Chappell Roan" ||
		tr.ExternalURL != "https://music.amazon.com/albums/B0HMXVCKG1?trackAsin=B0HMY12BH5" || tr.Images == "" {
		t.Errorf("track[0] = %+v", tr)
	}
	if !r.Tracks[1].IsExplicit {
		t.Error("track[1] should be explicit")
	}
	al := r.Albums[1]
	if al.ID != "B091BHTFTZ" || al.Name != "SOUR" || al.Type != "album" || al.Artists != "Olivia Rodrigo" || !al.IsExplicit {
		t.Errorf("album[1] = %+v", al)
	}
	ar := r.Artists[0]
	if ar.ID != "B06WP75FR9" || ar.Name != "Olivia Rodrigo" || ar.Type != "artist" || ar.ExternalURL != "https://music.amazon.com/artists/B06WP75FR9" {
		t.Errorf("artist[0] = %+v", ar)
	}
	if r.Playlists[0].Name != "REDISCOVER Olivia Rodrigo" || r.Playlists[1].Name != "Pop Culture" || r.Playlists[0].Type != "playlist" {
		t.Errorf("playlists = %+v", r.Playlists)
	}
	for _, list := range [][]string{{r.Tracks[0].ExternalURL, r.Tracks[1].ExternalURL, r.Albums[0].ExternalURL, r.Albums[1].ExternalURL, r.Artists[0].ExternalURL, r.Artists[1].ExternalURL, r.Playlists[0].ExternalURL, r.Playlists[1].ExternalURL}} {
		for _, u := range list {
			if strings.Contains(u, "/user-playlists/") || strings.Contains(u, "/stations/") {
				t.Errorf("hidden kind leaked: %s", u)
			}
		}
	}
}

func TestParseSearchResponseEdges(t *testing.T) {
	r, err := parseSearchResponse([]byte(`{"methods":[{"template":{"header":"Search","widgets":[]}}]}`))
	if err != nil || r.Tracks == nil || r.Albums == nil || r.Artists == nil || r.Playlists == nil {
		t.Errorf("empty results must be non-nil slices: %+v, %v", r, err)
	}
	if _, err := parseSearchResponse([]byte(`{"methods":[{"template":{"header":"Service error"}}]}`)); err == nil {
		t.Error("service error should fail")
	}
}
