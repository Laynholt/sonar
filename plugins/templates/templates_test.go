package templates_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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
	if m.Kind == plugin.KindDownloader {
		i := slices.IndexFunc(m.Settings, func(f plugin.Field) bool { return f.Key == "baseUrl" })
		if i < 0 || m.Settings[i].Type != "text" {
			t.Errorf("%s: want a baseUrl setting of type text, got %+v", dir, m.Settings)
		}
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
	}, parseManifest(t, filepath.Join("python", "downloader")).ID, true)
}

func TestGoDownloaderTemplate(t *testing.T) {
	checkDownloader(t, func(t *testing.T, dir string) {
		copyDir(t, filepath.Join("go", "downloader"), dir)
		buildGo(t, "downloader", dir)
	}, parseManifest(t, filepath.Join("go", "downloader")).ID, false)
}

// checkDownloader installs a downloader template with put into a fresh user folder, trusts it, points it at a
// local server and downloads one track and one missing file.
func checkDownloader(t *testing.T, put func(t *testing.T, dir string), id string, cutIsUnavailable bool) {
	t.Helper()
	body := bytes.Repeat([]byte("sonar"), 200_000) // 1 MB: several 256 KiB steps
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/Cut Artist - Cut Title.flac" { // promises 1000 bytes, sends 100 and drops the connection
			w.Header().Set("Content-Length", "1000")
			w.Write(make([]byte, 100))
			w.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
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

	// A body cut short is not a finished download: no file may be left, and the Python template calls the
	// site unavailable (-32001) so sonar tries another downloader.
	cut := filepath.Join(root, "out", "cut")
	_, err = d.Download(ctx, source.Request{Track: download.Track{Title: "Cut Title", Artist: "Cut Artist"}, PathNoExt: cut})
	if err == nil {
		t.Error("a short body counted as success")
	}
	if cutIsUnavailable && !errors.Is(err, source.ErrUnavailable) {
		t.Errorf("short body: %v, want source.ErrUnavailable", err)
	}
	for _, left := range []string{cut + ".flac", cut + ".flac.part"} {
		if _, err := os.Stat(left); err == nil {
			t.Errorf("%s left behind", left)
		}
	}
}

// The Python source and tagger templates answer initialize and one method each, with canned data.
func TestPythonSourceAndTaggerTemplates(t *testing.T) {
	if _, err := exec.LookPath("python"); err != nil {
		t.Skip("no python on PATH")
	}
	for _, tc := range []struct{ kind, method, params, want string }{
		{"source", "source.search", `{"query":"x","limit":3}`, "tracks"},
		{"tagger", "tag", `{"track":{"title":"t","artist":"a"},"ids":{}}`, "fields"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python", "main.py")
			cmd.Dir = filepath.Join("python", tc.kind)
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { in.Close(); cmd.Wait() }()
			fmt.Fprintln(in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"contract":1,"settings":{}}}`)
			fmt.Fprintf(in, `{"jsonrpc":"2.0","id":2,"method":%q,"params":%s}`+"\n", tc.method, tc.params)
			sc := bufio.NewScanner(out) // stdin stays open until both answers are read
			got := map[int64]plugin.Message{}
			for len(got) < 2 && sc.Scan() {
				var m plugin.Message
				if err := json.Unmarshal(sc.Bytes(), &m); err != nil || m.ID == nil {
					t.Fatalf("bad line %q: %v", sc.Text(), err)
				}
				got[*m.ID] = m
			}
			if got[1].Error != nil || got[1].Result == nil {
				t.Errorf("initialize: %+v", got[1])
			}
			var res map[string]json.RawMessage
			if m := got[2]; m.Error != nil || json.Unmarshal(m.Result, &res) != nil || res[tc.want] == nil {
				t.Errorf("%s: %+v, want a result with %q", tc.method, m, tc.want)
			}
		})
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
