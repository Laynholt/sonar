package resolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sonar/internal/download"
)

func TestLiveResolveCruelSummer(t *testing.T) {
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1 (and HTTPS_PROXY for Spotify) to hit Amazon and Spotify")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	tr := download.Track{ID: "1BxfuPKGuaTgP7aM0Bbdwr", Title: "Cruel Summer", Artist: "Taylor Swift", Album: "Lover",
		AlbumArtist: "Taylor Swift", DurationMS: 178426, Service: "spotify"}
	got := New(Live(), OpenCache(filepath.Join(t.TempDir(), "matches.json"))).Resolve(ctx, []download.Track{tr})[0]
	t.Logf("AmazonID=%q Via=%q Err=%v", got.AmazonID, got.Via, got.Err)
	for _, c := range got.Candidates {
		t.Logf("candidate %+v", c)
	}
	if got.AmazonID == "" || (got.Via != "title" && got.Via != "isrc") {
		t.Fatalf("want an AmazonID via title or isrc, got %+v", got)
	}
}
