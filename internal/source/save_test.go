package source

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sonar/internal/download"
)

func resp(status int, ct, cd, body string) *http.Response {
	h := http.Header{}
	if ct != "" {
		h.Set("Content-Type", ct)
	}
	if cd != "" {
		h.Set("Content-Disposition", cd)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range es {
		names = append(names, e.Name())
	}
	return names
}

func TestSaveExtensions(t *testing.T) {
	tests := []struct{ ct, cd, ext string }{
		{"audio/flac", "", "flac"},
		{"audio/mpeg", "", "mp3"},
		{"audio/mp4", "", "m4a"},
		{"audio/ogg", "", "ogg"},
		{"application/octet-stream", "attachment; filename*=UTF-8''A%20B.flac", "flac"},
		{"application/octet-stream", `attachment; filename="x.mp3"`, "mp3"},
	}
	for _, tt := range tests {
		dir := t.TempDir()
		base := filepath.Join(dir, "sub", "track")
		got, err := Save(resp(200, tt.ct, tt.cd, "audio-bytes"), base)
		if err != nil {
			t.Fatalf("%s: %v", tt.ct, err)
		}
		if want := base + "." + tt.ext; got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
		if b, _ := os.ReadFile(got); string(b) != "audio-bytes" {
			t.Fatalf("content = %q", b)
		}
		if n := dirNames(t, filepath.Dir(base)); len(n) != 1 {
			t.Fatalf("leftover files: %v", n)
		}
	}
}

func TestSaveRejectsNonAudio(t *testing.T) {
	dir := t.TempDir()
	_, err := Save(resp(200, "text/html", "", "<html>Just a moment...</html>"), filepath.Join(dir, "t"))
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "text/html") || strings.Contains(err.Error(), "Just a moment") {
		t.Fatalf("err = %v", err)
	}
	if n := dirNames(t, dir); len(n) != 0 {
		t.Fatalf("dir not empty: %v", n)
	}
}

func TestSaveRejectsArchive(t *testing.T) {
	dir := t.TempDir()
	_, err := Save(resp(200, "application/zip", "", "PK"), filepath.Join(dir, "t"))
	if err == nil || !strings.Contains(err.Error(), "archive") || errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if n := dirNames(t, dir); len(n) != 0 {
		t.Fatalf("dir not empty: %v", n)
	}
}

type cancelReader struct{ sent bool }

func (r *cancelReader) Read(p []byte) (int, error) {
	if r.sent {
		return 0, context.Canceled
	}
	r.sent = true
	return copy(p, make([]byte, 1024)), nil
}

func TestSaveCancelRemovesPart(t *testing.T) {
	dir := t.TempDir()
	r := resp(200, "audio/flac", "", "")
	r.Body = io.NopCloser(&cancelReader{})
	_, err := Save(r, filepath.Join(dir, "t"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if n := dirNames(t, dir); len(n) != 0 {
		t.Fatalf("dir not empty: %v", n)
	}
}

func TestCheckStatus(t *testing.T) {
	if err := CheckStatus("lucida", resp(200, "", "", "")); err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{403, 429, 502} {
		err := CheckStatus("lucida", resp(code, "", "", ""))
		if !errors.Is(err, ErrUnavailable) || !strings.HasPrefix(err.Error(), "lucida: HTTP ") {
			t.Fatalf("%d: %v", code, err)
		}
	}
	if err := CheckStatus("lucida", resp(404, "", "", "")); err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("404: %v", err)
	}
}

func TestSaveReportsProgress(t *testing.T) {
	var done, total int64
	ctx := download.WithProgress(context.Background(), func(d, n int64) { done, total = d, n })
	r := resp(200, "audio/flac", "", "abcdef")
	r.ContentLength = 6
	r.Request = httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
	if _, err := Save(r, filepath.Join(t.TempDir(), "x")); err != nil {
		t.Fatal(err)
	}
	if done != 6 || total != 6 {
		t.Fatalf("got %d of %d", done, total)
	}
}
