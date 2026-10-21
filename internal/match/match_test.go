package match

import "testing"

func TestBaseTitle(t *testing.T) {
	cases := map[string]string{
		"Global Warming (feat. Sensato)":   "Global Warming",
		"Song - Remastered 2011":           "Song",
		"Track [Live] (feat. X)":           "Track",
		"(What's The Story) Morning Glory": "(What's The Story) Morning Glory",
		"Plain":                            "Plain",
	}
	for in, want := range cases {
		if got := BaseTitle(in); got != want {
			t.Errorf("BaseTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRankPrefersSameSongAndArtist(t *testing.T) {
	src := Item{Kind: "track", Title: "Creep", Artist: "Radiohead", Explicit: true}
	got := Rank(src, []Item{
		{Kind: "track", Title: "Creep (Radiohead Cover)", Artist: "Daniel Chuer", URL: "cover"},
		{Kind: "album", Title: "Creep", Artist: "Radiohead", URL: "album"},
		{Kind: "track", Title: "Creep", Artist: "Radiohead", URL: "clean"},
		{Kind: "track", Title: "Creep", Artist: "Radiohead", Explicit: true, URL: "orig"},
	}, 4)
	if len(got) != 3 || got[0].URL != "orig" || got[1].URL != "clean" || got[2].URL != "cover" {
		t.Fatalf("unexpected order: %+v", got)
	}
	if got[0].Score < ConfidentScore || got[2].Score >= ConfidentScore {
		t.Errorf("scores: orig %.2f, cover %.2f", got[0].Score, got[2].Score)
	}
}

func TestScoreToleratesFeatSuffix(t *testing.T) {
	src := Item{Kind: "track", Title: "Global Warming (feat. Sensato)", Artist: "Pitbull, Sensato"}
	c := Item{Kind: "track", Title: "Global Warming", Artist: "Pitbull"}
	if s := Score(src, c); s < ConfidentScore {
		t.Errorf("score = %.2f, want >= %.2f", s, ConfidentScore)
	}
}

func TestRemasterBeatsOtherVersions(t *testing.T) {
	src := Item{Kind: "track", Title: "Never Gonna Give You Up", Artist: "Rick Astley"}
	got := Rank(src, []Item{
		{Kind: "track", Title: "Never Gonna Give You Up (Pianoforte)", Artist: "Rick Astley", URL: "piano"},
		{Kind: "track", Title: "Never Gonna Give You Up (2022 Remaster)", Artist: "Rick Astley", URL: "remaster"},
	}, 4)
	if got[0].URL != "remaster" || got[0].Score <= got[1].Score {
		t.Errorf("want remaster first and strictly higher: %+v", got)
	}
}
