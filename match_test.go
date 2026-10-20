package main

import (
	"os"
	"testing"
)

func TestParseSpotifyURL(t *testing.T) {
	cases := map[string]string{
		"https://open.spotify.com/track/4cOdK2wGLETKBW3PvgPWqT?si=abc":  "track 4cOdK2wGLETKBW3PvgPWqT",
		"https://open.spotify.com/intl-de/album/4aawyAB9vmqN3uQ7FjRGTy": "album 4aawyAB9vmqN3uQ7FjRGTy",
		"spotify:track:4cOdK2wGLETKBW3PvgPWqT":                          "track 4cOdK2wGLETKBW3PvgPWqT",
		"  https://open.spotify.com/album/4aawyAB9vmqN3uQ7FjRGTy  ":     "album 4aawyAB9vmqN3uQ7FjRGTy",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M":      "",
		"https://music.amazon.com/albums/B01DPROTRO":                    "",
	}
	for in, want := range cases {
		kind, id, err := parseSpotifyURL(in)
		got := ""
		if err == nil {
			got = kind + " " + id
		}
		if got != want {
			t.Errorf("parseSpotifyURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBaseTitle(t *testing.T) {
	cases := map[string]string{
		"Global Warming (feat. Sensato)":   "Global Warming",
		"Song - Remastered 2011":           "Song",
		"Track [Live] (feat. X)":           "Track",
		"(What's The Story) Morning Glory": "(What's The Story) Morning Glory",
		"Plain":                            "Plain",
	}
	for in, want := range cases {
		if got := baseTitle(in); got != want {
			t.Errorf("baseTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRankPrefersSameSongAndArtist(t *testing.T) {
	src := Item{Kind: "track", Title: "Creep", Artist: "Radiohead", Explicit: true}
	got := rank(src, []Item{
		{Kind: "track", Title: "Creep (Radiohead Cover)", Artist: "Daniel Chuer", URL: "cover"},
		{Kind: "album", Title: "Creep", Artist: "Radiohead", URL: "album"},
		{Kind: "track", Title: "Creep", Artist: "Radiohead", URL: "clean"},
		{Kind: "track", Title: "Creep", Artist: "Radiohead", Explicit: true, URL: "orig"},
	}, 4)
	if len(got) != 3 || got[0].URL != "orig" || got[1].URL != "clean" || got[2].URL != "cover" {
		t.Fatalf("unexpected order: %+v", got)
	}
	if got[0].Score < confidentScore || got[2].Score >= confidentScore {
		t.Errorf("scores: orig %.2f, cover %.2f", got[0].Score, got[2].Score)
	}
}

func TestScoreToleratesFeatSuffix(t *testing.T) {
	src := Item{Kind: "track", Title: "Global Warming (feat. Sensato)", Artist: "Pitbull, Sensato"}
	c := Item{Kind: "track", Title: "Global Warming", Artist: "Pitbull"}
	if s := score(src, c); s < confidentScore {
		t.Errorf("score = %.2f, want >= %.2f", s, confidentScore)
	}
}

func TestRemasterBeatsOtherVersions(t *testing.T) {
	src := Item{Kind: "track", Title: "Never Gonna Give You Up", Artist: "Rick Astley"}
	got := rank(src, []Item{
		{Kind: "track", Title: "Never Gonna Give You Up (Pianoforte)", Artist: "Rick Astley", URL: "piano"},
		{Kind: "track", Title: "Never Gonna Give You Up (2022 Remaster)", Artist: "Rick Astley", URL: "remaster"},
	}, 4)
	if got[0].URL != "remaster" || got[0].Score <= got[1].Score {
		t.Errorf("want remaster first and strictly higher: %+v", got)
	}
}

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

func TestParseSpotifyEmbed(t *testing.T) {
	page := []byte(`<html><script id="__NEXT_DATA__" type="application/json">{"props":{"pageProps":{"state":{"data":{"entity":
	 {"name":"Global Warming","subtitle":"Pitbull, Sensato","isExplicit":true,
	  "visualIdentity":{"image":[{"url":"small","maxWidth":64},{"url":"big","maxWidth":640}]}}}}}}}</script></html>`)
	it, err := parseSpotifyEmbed(page, "album", "4aawyAB9vmqN3uQ7FjRGTy")
	if err != nil {
		t.Fatal(err)
	}
	if it.Title != "Global Warming" || it.Artist != "Pitbull, Sensato" || it.Image != "big" || !it.Explicit {
		t.Errorf("got %+v", it)
	}
	if _, err := parseSpotifyEmbed([]byte(`<script id="__NEXT_DATA__" type="application/json">{"props":{}}</script>`), "track", "x"); err == nil {
		t.Error("want error when entity is missing")
	}
}

// Hits the real services: SONAR_LIVE=1 go test -run Live -v
func TestLiveFind(t *testing.T) {
	if os.Getenv("SONAR_LIVE") == "" {
		t.Skip("set SONAR_LIVE=1 to run against Spotify and Amazon")
	}
	for _, link := range []string{
		"https://open.spotify.com/track/4cOdK2wGLETKBW3PvgPWqT",
		"https://open.spotify.com/album/4aawyAB9vmqN3uQ7FjRGTy",
	} {
		res, err := (&App{}).Find(link)
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		t.Logf("%s — %s", res.Source.Artist, res.Source.Title)
		for _, m := range res.Matches {
			t.Logf("  %.2f %s — %s %s", m.Score, m.Artist, m.Title, m.URL)
		}
		if !res.Confident {
			t.Errorf("%s: no confident match", link)
		}
	}
}
