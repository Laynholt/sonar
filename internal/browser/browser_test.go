package browser

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testClient builds a client over a temp data dir and reports whether a browser is available at all.
func testClient(t *testing.T) *Client {
	t.Helper()
	if FindBrowser() == "" {
		t.Skip("no Chromium-based browser installed")
	}
	t.Setenv("SONAR_BROWSER_TEST", "1") // marks the run for easier process triage
	return New(t.TempDir())
}

func TestFindBrowser(t *testing.T) {
	if FindBrowser() == "" {
		t.Skip("no Chromium-based browser installed")
	}
}

func TestFetchAndDownload(t *testing.T) {
	const payload = "0123456789abcdef" // 16 bytes: small enough to stream, big enough to prove content
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/data.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/file":
			w.Header().Set("Content-Type", "audio/flac")
			w.Header().Set("Content-Disposition", `attachment; filename="song.flac"`)
			http.ServeContent(w, r, "song.flac", time.Now(), stringsReader(payload))
		case "/range":
			if r.Header.Get("Range") == "" {
				t.Errorf("Range header missing")
			}
			w.Header().Set("Content-Type", "audio/flac")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("x"))
		default:
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><head><title>test site</title></head><body>ok</body></html>"))
		}
	}))
	defer srv.Close()

	c := testClient(t)
	defer c.Close()
	oldWait := challengeWait
	challengeWait = 5 * time.Second // no challenge on localhost, but keep the budget short
	t.Cleanup(func() { challengeWait = oldWait })

	ctx := context.Background()
	page, err := c.Page(ctx, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	status, body, err := c.Fetch(ctx, page, http.MethodGet, srv.URL+"/data.json", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != 200 || string(body) != `{"ok":true}` {
		t.Fatalf("fetch: %d %s", status, body)
	}

	finalURL, ct, err := c.ProbeDownload(ctx, page, srv.URL+"/range")
	if err != nil {
		t.Fatal(err)
	}
	if ct != "audio/flac" || finalURL != srv.URL+"/range" {
		t.Fatalf("probe: %q %q", ct, finalURL)
	}

	path, derr := c.Download(ctx, srv.URL+"/file", filepath.Join(t.TempDir(), "job"), func(done, total int64) {})
	if derr != nil {
		t.Fatal(derr)
	}
	if b, err := os.ReadFile(path); err != nil || string(b) != payload {
		t.Fatalf("download: %v %q", err, b)
	}
}

func stringsReader(s string) *io.SectionReader {
	return io.NewSectionReader(strings.NewReader(s), 0, int64(len(s)))
}
