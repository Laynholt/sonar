package spotify

import (
	"encoding/json"
	"os"
	"testing"

	"sonar/internal/model"
)

func TestParseTopTracks(t *testing.T) {
	raw, err := os.ReadFile("testdata/artist_overview.json")
	if err != nil {
		t.Fatal(err)
	}
	var overview map[string]interface{}
	if err := json.Unmarshal(raw, &overview); err != nil {
		t.Fatal(err)
	}
	got := parseTopTracks(overview)
	if len(got) != 10 {
		t.Fatalf("tracks = %d, want 10", len(got))
	}
	first := got[0]
	if first.ID != "3C0nOe05EIt1390bVABLyN" || first.Name != "On The Floor" ||
		first.AlbumID != "3sysiYphqNRQw7VKLCg1yE" || first.DurationMS != 284866 {
		t.Errorf("first = %+v", first)
	}
	if first.Service != model.Spotify || first.Artists != "Jennifer Lopez, Pitbull" || first.Images == "" ||
		first.DiscNumber != 1 || first.ExternalURL != "https://open.spotify.com/track/3C0nOe05EIt1390bVABLyN" {
		t.Errorf("first = %+v", first)
	}
	if first.TrackNumber != 0 || first.ReleaseDate != "" || first.AlbumName != "" {
		t.Errorf("overview must leave gaps for fetchTrack: %+v", first)
	}
	if !got[4].IsExplicit || got[0].IsExplicit {
		t.Errorf("explicit: %v %v", got[0].IsExplicit, got[4].IsExplicit)
	}
	if parseTopTracks(map[string]interface{}{}) != nil {
		t.Error("empty overview must give nil")
	}
}
