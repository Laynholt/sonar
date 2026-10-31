package templates_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"sonar/internal/download"
	"sonar/internal/plugin"
	"sonar/internal/source"
)

func parseManifest(t *testing.T, dir string) plugin.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.ParseManifest(raw)
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	if m.Contract != plugin.Contract {
		t.Errorf("%s: contract %d, want %d", dir, m.Contract, plugin.Contract)
	}
	return m
}

// buildGo builds the Go template kind into dir under the name its manifest runs.
func buildGo(t *testing.T, kind, dir string) {
	t.Helper()
	m := parseManifest(t, filepath.Join("go", kind))
	if string(m.Kind) != kind {
		t.Errorf("kind %q", m.Kind)
	}
	out := filepath.Join(dir, m.Run[0])
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	if b, err := exec.Command("go", "build", "-o", out, "./go/"+kind).CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
}

func TestGoTemplatesBuild(t *testing.T) {
	for _, kind := range []string{"downloader", "source", "tagger"} {
		t.Run(kind, func(t *testing.T) { buildGo(t, kind, t.TempDir()) })
	}
}

func TestPythonTemplatesManifests(t *testing.T) {
	for _, kind := range []string{"downloader", "source", "tagger"} {
		t.Run(kind, func(t *testing.T) {
			dir := filepath.Join("python", kind)
			m := parseManifest(t, dir)
			if string(m.Kind) != kind {
				t.Errorf("kind %q", m.Kind)
			}
			if len(m.Run) != 2 || m.Run[0] != "python" {
				t.Fatalf("run %v, want python and a script", m.Run)
			}
			if _, err := os.Stat(filepath.Join(dir, m.Run[1])); err != nil {
				t.Error(err)
			}
		})
	}
}

// The Python downloader template is the "third-party plugin" of the plan: copied into a user folder, trusted,
// pointed at a server, it downloads through the Downloader adapter with progress.
func TestPythonDownloaderTemplate(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("no python on PATH")
	}
	checkDownloader(t, func(t *testing.T, dir string) {
		copyDir(t, filepath.Join("python", "downloader"), dir)
	}, parseManifest(t, filepath.Join("python", "downloader")).ID)
}

func TestGoDownloaderTemplate(t *testing.T) {
	checkDownloader(t, func(t *testing.T, dir string) {
		copyDir(t, filepath.Join("go", "downloader"), dir)
		buildGo(t, "downloader", dir)
	}, parseManifest(t, filepath.Join("go", "downloader")).ID)
}

// checkDownloader installs a downloader template with put into a fresh user folder, trusts it, points it at a
// local server and downloads one track and one missing file.
func checkDownloader(t *testing.T, put func(t *testing.T, dir string), id string) {
	t.Helper()
	body := bytes.Repeat([]byte("sonar"), 200_000) // 1 MB: several 256 KiB steps
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/Сome Artist - Some Title.flac" { // the C is Cyrillic: names are UTF-8 end to end
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()

	root := t.TempDir()
	userDir := filepath.Join(root, "user")
	put(t, filepath.Join(userDir, id))
	r := plugin.OpenRegistry(filepath.Join(root, "data"), filepath.Join(root, "builtin"), userDir, nil,
		func(string) plugin.InitParams { return plugin.InitParams{Lang: "en"} }, t.Logf)
	t.Cleanup(r.Close)
	if err := r.SetEnabled(id, true, false); err != plugin.ErrNeedsTrust {
		t.Fatalf("enabling a third-party plugin without confirmation: %v, want ErrNeedsTrust", err)
	}
	if err := r.SetEnabled(id, true, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSettings(id, map[string]any{"baseUrl": srv.URL}); err != nil {
		t.Fatal(err)
	}
	var d *plugin.Downloader
	for _, c := range r.Downloaders() {
		if c.ID == id {
			d = c
		}
	}
	if d == nil {
		t.Fatal("the template is not listed as a downloader")
	}
	if d.Info.Accepts != "any" {
		t.Errorf("accepts %q", d.Info.Accepts)
	}

	var ticks int
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx = download.WithProgress(ctx, func(done, total int64) {
		if done > 0 {
			ticks++
		}
	})
	path := filepath.Join(root, "out", "song")
	got, err := d.Download(ctx, source.Request{
		Track:     download.Track{ID: "x", Title: "Some Title", Artist: "Сome Artist"},
		PathNoExt: path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != path+".flac" {
		t.Errorf("path %q, want %q", got, path+".flac")
	}
	if b, err := os.ReadFile(got); err != nil || !bytes.Equal(b, body) {
		t.Errorf("file: %d bytes, %v", len(b), err)
	}
	if ticks < 1 {
		t.Error("no progress note reached the queue")
	}

	// A missing file is an ordinary track error: not "unavailable", which would bench the downloader.
	_, err = d.Download(ctx, source.Request{Track: download.Track{Title: "Nope", Artist: "Nobody"}, PathNoExt: filepath.Join(root, "out", "nope")})
	if err == nil || !strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "unavailable") {
		t.Errorf("missing file: %v", err)
	}
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	if err := os.MkdirAll(to, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "main.go" { // the Go template is built, not copied
			continue
		}
		b, err := os.ReadFile(filepath.Join(from, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
