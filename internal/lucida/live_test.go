package lucida

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLiveDownload(t *testing.T) {
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1 to hit lucida.to")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	got, err := New().Download(ctx, "https://music.amazon.com/tracks/B00A3SGXAA", filepath.Join(t.TempDir(), "song"), func(s string) { t.Log(s) })
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(got) != ".flac" || fi.Size() < 1<<20 {
		t.Fatalf("%s: %d bytes", got, fi.Size())
	}
	t.Logf("%s: %d bytes", got, fi.Size())
}
