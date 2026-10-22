package ffinstall

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"sonar/internal/httpx"
)

func TestLatestPicksHighestVersion(t *testing.T) {
	serve := func(names ...string) *httptest.Server {
		type asset struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		}
		var assets []asset
		for i, n := range names {
			assets = append(assets, asset{n, "http://dl/" + n, int64(i + 1)})
		}
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"assets": assets})
		}))
	}

	srv := serve(
		"ffmpeg-n8.1-latest-win64-gpl-shared-8.1.zip",
		"ffmpeg-n9.0-latest-win64-gpl-shared-9.0.zip",
		"ffmpeg-master-latest-win64-gpl-shared.zip",
		"ffmpeg-n9.0-latest-win64-lgpl-shared-9.0.zip",
		"checksums.sha256",
	)
	defer srv.Close()
	archive, sums, err := Latest(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Name != "ffmpeg-n9.0-latest-win64-gpl-shared-9.0.zip" || archive.URL != "http://dl/"+archive.Name || archive.Size != 2 {
		t.Errorf("archive = %+v", archive)
	}
	if sums.Name != "checksums.sha256" || sums.URL != "http://dl/checksums.sha256" {
		t.Errorf("sums = %+v", sums)
	}

	// 10.0 must beat 9.0 numerically, not as a string.
	srv2 := serve("ffmpeg-n9.0-latest-win64-gpl-shared-9.0.zip", "ffmpeg-n10.0-latest-win64-gpl-shared-10.0.zip", "checksums.sha256")
	defer srv2.Close()
	if archive, _, err = Latest(context.Background(), srv2.Client(), srv2.URL); err != nil || archive.Name != "ffmpeg-n10.0-latest-win64-gpl-shared-10.0.zip" {
		t.Errorf("got %+v, %v", archive, err)
	}

	for _, names := range [][]string{
		{"ffmpeg-master-latest-win64-gpl-shared.zip", "checksums.sha256"},
		{"ffmpeg-n9.0-latest-win64-gpl-shared-9.0.zip"}, // no sums
	} {
		bad := serve(names...)
		if _, _, err := Latest(context.Background(), bad.Client(), bad.URL); err == nil {
			t.Errorf("%v: want error", names)
		}
		bad.Close()
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer down.Close()
	if _, _, err := Latest(context.Background(), down.Client(), down.URL); err == nil {
		t.Error("HTTP 403: want error")
	}
}

func TestLiveLatest(t *testing.T) {
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1")
	}
	archive, sums, err := Latest(context.Background(), httpx.Client(), ReleaseAPI)
	if err != nil {
		t.Fatal(err)
	}
	if !archiveRe.MatchString(archive.Name) || archive.URL == "" || archive.Size <= 0 {
		t.Errorf("archive = %+v", archive)
	}
	if sums.Name != "checksums.sha256" || sums.URL == "" {
		t.Errorf("sums = %+v", sums)
	}
	t.Logf("%s (%d bytes)", archive.Name, archive.Size)
}
