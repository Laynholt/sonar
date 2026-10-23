package ffinstall

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

const zipName = "ffmpeg-n9.0-latest-win64-gpl-shared-9.0.zip"

func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.Create(name) // zip names always use forward slashes
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(files[name]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func goodZip(t *testing.T) []byte {
	return buildZip(t, map[string]string{
		"ffmpeg-n9.0-latest-win64-gpl-shared-9.0/bin/ffmpeg.exe":     "new-ffmpeg",
		"ffmpeg-n9.0-latest-win64-gpl-shared-9.0/bin/avcodec-61.dll": "dll",
		"ffmpeg-n9.0-latest-win64-gpl-shared-9.0/bin/ffprobe.exe":    "probe",
		"ffmpeg-n9.0-latest-win64-gpl-shared-9.0/doc/x.txt":          "doc",
	})
}

// fixture serves archive bytes at /zip and a checksums file at /sums.
type fixture struct {
	srv          *httptest.Server
	archive, sum Asset
	zipHandler   http.HandlerFunc // overrides the /zip response when set
}

func newFixture(t *testing.T, body []byte, sumHex string) *fixture {
	t.Helper()
	if sumHex == "" {
		h := sha256.Sum256(body)
		sumHex = hex.EncodeToString(h[:])
	}
	f := &fixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/zip", func(w http.ResponseWriter, r *http.Request) {
		if f.zipHandler != nil {
			f.zipHandler(w, r)
			return
		}
		w.Write(body)
	})
	mux.HandleFunc("/sums", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  other.zip\n%s  %s\n", strings.Repeat("0", 64), sumHex, zipName)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	f.archive = Asset{Name: zipName, URL: f.srv.URL + "/zip", Size: int64(len(body))}
	f.sum = Asset{Name: "checksums.sha256", URL: f.srv.URL + "/sums"}
	return f
}

func stubVerify(t *testing.T, err error) {
	old := verify
	verify = func(_ context.Context, p string) error {
		if _, serr := os.Stat(p); serr != nil {
			return serr
		}
		return err
	}
	t.Cleanup(func() { verify = old })
}

func names(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestInstallUnpacksOnlyFFmpegAndDLLs(t *testing.T) {
	stubVerify(t, nil)
	f := newFixture(t, goodZip(t), "")
	dir := filepath.Join(t.TempDir(), "app", "ffmpeg")
	var got []Progress
	path, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, dir, func(p Progress) { got = append(got, p) })
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "ffmpeg.exe") {
		t.Errorf("path = %q", path)
	}
	if want := []string{"avcodec-61.dll", "ffmpeg.exe", Manifest}; !reflect.DeepEqual(names(t, dir), want) {
		t.Errorf("dir = %v, want %v", names(t, dir), want)
	}
	if b, _ := os.ReadFile(path); string(b) != "new-ffmpeg" {
		t.Errorf("ffmpeg.exe = %q", b)
	}
	man, _ := os.ReadFile(filepath.Join(dir, Manifest))
	if lines := strings.Fields(string(man)); !reflect.DeepEqual(slices.Sorted(slices.Values(lines)), []string{"avcodec-61.dll", "ffmpeg.exe"}) {
		t.Errorf("manifest = %q", man)
	}
	if left := names(t, filepath.Dir(dir)); !reflect.DeepEqual(left, []string{"ffmpeg"}) {
		t.Errorf("parent holds %v", left)
	}

	var steps []string
	for _, p := range got {
		if len(steps) == 0 || steps[len(steps)-1] != p.Step {
			steps = append(steps, p.Step)
		}
	}
	if !reflect.DeepEqual(steps, []string{"download", "install"}) {
		t.Fatalf("steps = %v", steps)
	}
	var lastDl Progress
	for _, p := range got {
		if p.Step == "download" {
			lastDl = p
		}
	}
	if lastDl.Total == 0 || lastDl.Done != lastDl.Total {
		t.Errorf("last download = %+v", lastDl)
	}
	if last := got[len(got)-1]; last.Step != "install" || last.Done != last.Total || last.Total != 2 {
		t.Errorf("last = %+v", last)
	}
}

func TestInstallChecksumMismatch(t *testing.T) {
	stubVerify(t, nil)
	f := newFixture(t, goodZip(t), strings.Repeat("a", 64))
	parent := t.TempDir()
	_, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(parent, "ffmpeg"), nil)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if left := names(t, parent); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

func TestInstallNoFFmpegInZip(t *testing.T) {
	stubVerify(t, nil)
	f := newFixture(t, buildZip(t, map[string]string{"x/bin/avcodec-61.dll": "d", "x/doc/a.txt": "t"}), "")
	parent := t.TempDir()
	if _, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(parent, "ffmpeg"), nil); err == nil {
		t.Fatal("want error")
	}
	if left := names(t, parent); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

func TestInstallTooLarge(t *testing.T) {
	stubVerify(t, nil)
	body := goodZip(t)

	t.Run("declared size", func(t *testing.T) {
		f := newFixture(t, body, "")
		f.archive.Size = MaxSize + 1
		if _, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(t.TempDir(), "ffmpeg"), nil); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("content length", func(t *testing.T) {
		f := newFixture(t, body, "")
		f.archive.Size = 0
		f.zipHandler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(MaxSize+1))
			w.WriteHeader(200)
		}
		if _, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(t.TempDir(), "ffmpeg"), nil); err == nil {
			t.Fatal("want error")
		}
	})
	t.Run("streamed past the cap", func(t *testing.T) {
		if testing.Short() {
			t.Skip("writes 300 MB")
		}
		f := newFixture(t, body, "")
		f.archive.Size = 0
		f.zipHandler = func(w http.ResponseWriter, r *http.Request) {
			chunk := make([]byte, 1<<20)
			for i := 0; i <= MaxSize>>20; i++ { // chunked: no Content-Length
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		}
		parent := t.TempDir()
		_, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(parent, "ffmpeg"), nil)
		if err == nil || !strings.Contains(err.Error(), "too large") {
			t.Fatalf("err = %v", err)
		}
		if left := names(t, parent); len(left) != 0 {
			t.Errorf("leftovers: %v", left)
		}
	})
}

func TestInstallCancelLeavesNothing(t *testing.T) {
	stubVerify(t, nil)
	body := goodZip(t)
	f := newFixture(t, body, "")
	started := make(chan struct{})
	f.zipHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body[:10])
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "ffmpeg")
	os.Mkdir(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "ffmpeg.exe"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, Manifest), []byte("ffmpeg.exe\n"), 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	_, err := Install(ctx, f.srv.Client(), f.archive, f.sum, dir, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ffmpeg.exe")); string(b) != "old" {
		t.Errorf("ffmpeg.exe = %q", b)
	}
	if left := names(t, parent); !reflect.DeepEqual(left, []string{"ffmpeg"}) {
		t.Errorf("parent holds %v", left)
	}
}

func TestInstallStall(t *testing.T) {
	stubVerify(t, nil)
	old := stallTimeout
	stallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })

	body := goodZip(t)
	f := newFixture(t, body, "")
	f.zipHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Write(body[:10])
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}
	parent := t.TempDir()
	_, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(parent, "ffmpeg"), nil)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("err = %v", err)
	}
	if left := names(t, parent); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

func TestInstallReplacesOwnFilesOnly(t *testing.T) {
	stubVerify(t, nil)
	f := newFixture(t, goodZip(t), "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "ffmpeg.exe"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, "old.dll"), []byte("old"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644)
	os.WriteFile(filepath.Join(dir, Manifest), []byte("ffmpeg.exe\nold.dll\n"), 0o644)

	if _, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, dir, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"avcodec-61.dll", "ffmpeg.exe", "notes.txt", Manifest}; !reflect.DeepEqual(names(t, dir), want) {
		t.Errorf("dir = %v, want %v", names(t, dir), want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "ffmpeg.exe")); string(b) != "new-ffmpeg" {
		t.Errorf("ffmpeg.exe = %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); string(b) != "mine" {
		t.Errorf("notes.txt = %q", b)
	}
}

func TestInstallRefusesForeignFolder(t *testing.T) {
	stubVerify(t, nil)
	f := newFixture(t, goodZip(t), "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644)
	if _, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, dir, nil); !errors.Is(err, ErrNotEmpty) {
		t.Fatalf("err = %v", err)
	}
}

func TestCanInstall(t *testing.T) {
	root := t.TempDir()
	if err := CanInstall(filepath.Join(root, "missing")); err != nil {
		t.Errorf("missing: %v", err)
	}
	empty := filepath.Join(root, "empty")
	os.Mkdir(empty, 0o755)
	if err := CanInstall(empty); err != nil {
		t.Errorf("empty: %v", err)
	}
	own := filepath.Join(root, "own")
	os.Mkdir(own, 0o755)
	os.WriteFile(filepath.Join(own, Manifest), nil, 0o644)
	os.WriteFile(filepath.Join(own, "ffmpeg.exe"), nil, 0o644)
	if err := CanInstall(own); err != nil {
		t.Errorf("own: %v", err)
	}
	foreign := filepath.Join(root, "foreign")
	os.Mkdir(foreign, 0o755)
	os.WriteFile(filepath.Join(foreign, "a.txt"), nil, 0o644)
	if err := CanInstall(foreign); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("foreign: %v", err)
	}
}

func TestInstallVerifyFails(t *testing.T) {
	stubVerify(t, errors.New("boom"))
	f := newFixture(t, goodZip(t), "")
	parent := t.TempDir()
	_, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(parent, "ffmpeg"), nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
	if left := names(t, parent); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
}

// installBad runs Install on a zip and checks it errors with nothing left
// in the parent and nothing written above it.
func installBad(t *testing.T, files map[string]string, wantErr string) {
	t.Helper()
	stubVerify(t, nil)
	f := newFixture(t, buildZip(t, files), "")
	root := t.TempDir()
	parent := filepath.Join(root, "app")
	_, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(parent, "ffmpeg"), nil)
	if err == nil || !strings.Contains(err.Error(), wantErr) {
		t.Fatalf("err = %v, want %q", err, wantErr)
	}
	if left := names(t, parent); len(left) != 0 {
		t.Errorf("leftovers: %v", left)
	}
	if left := names(t, root); !reflect.DeepEqual(left, []string{"app"}) {
		t.Errorf("root holds %v", left)
	}
}

func TestInstallRejectsPathTraversalInZip(t *testing.T) {
	installBad(t, map[string]string{
		"x/bin/ffmpeg.exe":        "f",
		`x/bin/..\..\..\evil.dll`: "evil", // backslashes are separators to filepath on Windows
	}, "unsafe")
}

func TestInstallRejectsDuplicateNames(t *testing.T) {
	installBad(t, map[string]string{
		"x/bin/ffmpeg.exe": "f",
		"a/bin/x.dll":      "1",
		"b/bin/x.dll":      "2",
	}, "duplicate")
	installBad(t, map[string]string{
		"x/bin/ffmpeg.exe": "f",
		"a/bin/X.dll":      "1",
		"b/bin/x.dll":      "2",
	}, "duplicate")
}

func TestInstallCapsUnpackedSize(t *testing.T) {
	old := maxUnpacked
	maxUnpacked = 5
	t.Cleanup(func() { maxUnpacked = old })
	installBad(t, map[string]string{"x/bin/ffmpeg.exe": "0123456789"}, "too large")
}

func TestInstallChecksumsHostHangs(t *testing.T) {
	stubVerify(t, nil)
	old := sumsTimeout
	sumsTimeout = 50 * time.Millisecond
	t.Cleanup(func() { sumsTimeout = old })
	f := newFixture(t, goodZip(t), "")
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer hang.Close()
	f.sum.URL = hang.URL
	if _, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(t.TempDir(), "ffmpeg"), nil); err == nil {
		t.Fatal("want error")
	}
}

func TestInstallSweepsStaleTemps(t *testing.T) {
	parent := t.TempDir()
	os.WriteFile(filepath.Join(parent, "ffmpeg-123.zip.tmp"), []byte("half"), 0o644)
	os.MkdirAll(filepath.Join(parent, "ffmpeg-456.tmp", "sub"), 0o755)
	os.WriteFile(filepath.Join(parent, "ffmpeg-456.tmp", "ffmpeg.exe"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(parent, "keep.tmp"), []byte("mine"), 0o644)

	f := newFixture(t, goodZip(t), "")
	f.zipHandler = func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }
	if _, err := Install(context.Background(), f.srv.Client(), f.archive, f.sum, filepath.Join(parent, "ffmpeg"), nil); err == nil {
		t.Fatal("want error")
	}
	if left := names(t, parent); !reflect.DeepEqual(left, []string{"keep.tmp"}) {
		t.Errorf("parent holds %v", left)
	}
}
