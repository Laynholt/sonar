package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"log"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	goruntime "runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	flac "github.com/go-flac/go-flac"

	"sonar/internal/download"
	"sonar/internal/httpx"
	"sonar/internal/plugin"
	"sonar/internal/source"
	"sonar/internal/tags"
)

// fakeExe is the fake plugin from internal/plugin/testdata, built once for every test here.
var fakeExe string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "sonar-app-test")
	if err != nil {
		panic(err)
	}
	fakeExe = filepath.Join(tmp, "fake")
	if goruntime.GOOS == "windows" {
		fakeExe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", fakeExe, "./internal/plugin/testdata/fake").CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build fake: %v\n%s", err, out))
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// plug is a fake plugin's manifest. The fake reads its settings' defaults to know how to behave.
type plug map[string]any

func fakePlug(id, name, kind string, set map[string]any) plug {
	fields := []map[string]any{}
	for _, k := range slices.Sorted(maps.Keys(set)) {
		fields = append(fields, map[string]any{"key": k, "type": "text", "default": set[k]})
	}
	return plug{"id": id, "name": name, "version": "1.0.0", "kind": kind, "run": []string{fakeExe}, "settings": fields}
}

// srcPlug is a source whose links are https://<service>/...
func srcPlug(service, label string, set map[string]any, can ...string) plug {
	p := fakePlug(service, label, "source", set)
	p["source"] = map[string]any{"service": service, "label": label, "links": []string{"https://" + service + "/.*"}, "can": can}
	return p
}

// dlPlug is a downloader taking Amazon ids, with today's guard.
func dlPlug(id, name string, set map[string]any) plug {
	p := fakePlug(id, name, "downloader", set)
	p["downloader"] = map[string]any{"accepts": "amazon", "guard": map[string]any{"limit": 50, "waits": []int{15, 30, 60, 120}, "wait": 30}}
	return p
}

func tagPlug(id, needs string, set map[string]any) plug {
	p := fakePlug(id, id, "tagger", set)
	p["tagger"] = map[string]any{"fields": []string{"genre"}, "needs": needs}
	return p
}

var (
	spotifyCan = []string{"metadata", "search", "searchByType", "isrc", "preview", "parseLink"}
	amazonCan  = []string{"metadata", "search", "find", "tracks", "album", "preview", "parseLink"}
)

// stdPlugs are today's built-ins as fakes: the Spotify and Amazon sources (Amazon finds nothing, so a Spotify
// track lands in review) and two downloaders that "download" a copy of file.
func stdPlugs(file string) []plug {
	return []plug{
		srcPlug("spotify", "Spotify", nil, spotifyCan...),
		srcPlug("amazon", "Amazon Music", map[string]any{"find": []any{}}, amazonCan...),
		dlPlug("lucida", "Lucida", map[string]any{"copyFrom": file}),
		dlPlug("doubledouble", "DoubleDouble", map[string]any{"copyFrom": file}),
	}
}

// newPluginApp wires an App on dir the way init does, with plugs as its built-in plugins. A nil fetch uses the App's own.
func newPluginApp(t *testing.T, dir string, fetch download.Fetch, plugs ...plug) *App {
	t.Helper()
	builtin, sums := t.TempDir(), map[string]string{}
	for _, p := range plugs {
		id := p["id"].(string)
		raw, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		pd := filepath.Join(builtin, id)
		if err := os.MkdirAll(pd, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pd, "plugin.json"), raw, 0o644); err != nil {
			t.Fatal(err)
		}
		if sums[id], err = plugin.Checksum(pd); err != nil {
			t.Fatal(err)
		}
	}
	a := &App{dataDir: dir, findFFmpeg: func(string) string { return "" }}
	a.openPlugins(builtin, filepath.Join(t.TempDir(), "user"), sums)
	a.migrateAtStartup() // as init does
	if fetch == nil {
		fetch = a.fetch
	}
	a.startQueue(fetch)
	t.Cleanup(func() { a.shutdown(nil) })
	return a
}

// testFLAC writes a tiny FLAC holding tg and, when not nil, that front cover.
func testFLAC(t *testing.T, tg tags.Tags, cover []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tiny.flac")
	f := &flac.File{Meta: []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}}, Frames: []byte("\xff\xf8frames")}
	if err := f.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := tags.Write(context.Background(), "", p, tg, cover); err != nil {
		t.Fatal(err)
	}
	return p
}

func testPNG(t *testing.T, size int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, size, size))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

var amazonTrack = download.Track{ID: "B00A3SGXAA", Title: "T", Artist: "Ar", Album: "Al", TrackNumber: 3, Service: "amazon"}

// fetchTrack runs a.fetch through lucida into a temp folder and returns the path, the statuses and the error.
func fetchTrack(t *testing.T, a *App, tr download.Track, o download.Options) (string, []string, error) {
	t.Helper()
	var mu sync.Mutex
	var st []string
	p, err := a.fetch(context.Background(), "lucida", tr, nil, o, filepath.Join(t.TempDir(), "x"), func(m string) {
		mu.Lock()
		defer mu.Unlock()
		st = append(st, m)
	})
	mu.Lock()
	defer mu.Unlock()
	return p, slices.Clone(st), err
}

func readFile(t *testing.T, path string) tags.Tags {
	t.Helper()
	got, _, err := tags.Read(context.Background(), "", path)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// swap sets *p to v until the test ends.
func swap[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func view(t *testing.T, a *App, id string) PluginView {
	t.Helper()
	for _, v := range a.ListPlugins() {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("no plugin %q", id)
	return PluginView{}
}

func TestGetMetadataViaSource(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, srcPlug("fake", "Fake", nil, "metadata"), srcPlug("other", "Other", nil, "search"))
	got, err := a.GetMetadata("https://fake/album/1")
	if err != nil || got != `{"url":"https://fake/album/1"}` {
		t.Fatalf("got %s, %v", got, err)
	}
	if _, err := a.GetMetadata("https://other/album/1"); err == nil || err.Error() != "Unsupported link. Paste a link from Fake." {
		t.Fatalf("a source that cannot give metadata: %v", err)
	}
}

func TestListSourcesAndSearch(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	want := []SourceView{{"amazon", "Amazon Music", "", amazonCan}, {"spotify", "Spotify", "", spotifyCan}}
	if got := a.ListSources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("ListSources = %+v", got)
	}
	r, err := a.Search("spotify", "q", 0)
	if err != nil || len(r.Tracks) != 1 || r.Tracks[0].Name != "q" || r.Tracks[0].Duration != 10 { // the fake answers the limit as Duration
		t.Fatalf("Search = %+v, %v", r, err)
	}
	page, err := a.SearchByType("spotify", "q", "album", 0, 5)
	if err != nil || len(page) != 1 || page[0].ID != "5" || page[0].Type != "album" || page[0].Duration != 50 {
		t.Fatalf("SearchByType = %+v, %v", page, err)
	}
	if _, err := a.Search("tidal", "q", 0); err == nil {
		t.Fatal("searched a service with no source")
	}
	if _, err := a.SearchByType("amazon", "q", "track", 0, 0); !errors.Is(err, plugin.ErrCannot) {
		t.Fatalf("SearchByType on a source that cannot: %v", err)
	}
	if u, err := a.GetPreviewURL("amazon", "B00A3SGXAA"); u != "http://preview/B00A3SGXAA" || err != nil {
		t.Fatalf("preview = %q, %v", u, err)
	}
}

func TestFetchPipeline(t *testing.T) {
	file := testFLAC(t, tags.Tags{"title": "Own title"}, nil)
	// t2 answers only once the App says it is waiting for tags: the handshake makes that status certain.
	waiting := filepath.Join(t.TempDir(), "waiting")
	a := newPluginApp(t, t.TempDir(), nil, append(stdPlugs(file),
		tagPlug("t1", "", map[string]any{"tagFields": map[string]any{"genre": "Pop", "comment": "c1"}}),
		tagPlug("t2", "", map[string]any{"tagFields": map[string]any{"genre": "Rock"}, "waitFor": waiting}))...)
	var mu sync.Mutex
	var st []string
	p, err := a.fetch(context.Background(), "lucida", amazonTrack, nil, download.Options{Format: "original"}, filepath.Join(t.TempDir(), "x"), func(m string) {
		mu.Lock()
		defer mu.Unlock()
		st = append(st, m)
		if m == "Looking up tags…" {
			os.WriteFile(waiting, nil, 0o644)
		}
	})
	if err != nil || !strings.HasSuffix(p, ".flac") {
		t.Fatalf("path %q, err %v", p, err)
	}
	got := readFile(t, p)
	for k, v := range map[string]string{"title": "Own title", "artist": "Ar", "album": "Al", "track": "3", "genre": "Rock", "comment": "c1",
		"id_amazon": "B00A3SGXAA"} { // the taggers get the track's own id
		if got[k] != v {
			t.Errorf("%s = %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
	mu.Lock()
	if !slices.Contains(st, "Looking up tags…") {
		t.Errorf("statuses %q", st)
	}
	mu.Unlock()

	// Taggers done before the download: nothing to wait for, so no status. The download waits for the tagger
	// to be called, then a second for its answer to arrive.
	tagged := filepath.Join(t.TempDir(), "tagged")
	a = newPluginApp(t, t.TempDir(), nil,
		srcPlug("amazon", "Amazon Music", nil, amazonCan...),
		dlPlug("lucida", "Lucida", map[string]any{"copyFrom": file, "waitFor": tagged, "downloadSleep": "1s"}),
		tagPlug("t1", "", map[string]any{"tagFields": map[string]any{"genre": "Pop"}, "touch": tagged}))
	p, st2, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "original"})
	if err != nil || readFile(t, p)["genre"] != "Pop" || slices.Contains(st2, "Looking up tags…") {
		t.Errorf("err %v, statuses %q", err, st2)
	}
}

// A tagger slower than the budget is dropped and the track keeps its own tags. TestFetchPipeline covers the status.
func TestFetchTaggerBudget(t *testing.T) {
	swap(t, &tagBudget, 2*time.Second)
	a := newPluginApp(t, t.TempDir(), nil, append(stdPlugs(testFLAC(t, nil, nil)),
		tagPlug("slow", "", map[string]any{"tagFields": map[string]any{"genre": "Rock"}, "tagSleep": "30s"}))...)
	start := time.Now()
	p, _, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "original"})
	if err != nil || time.Since(start) > 20*time.Second {
		t.Fatalf("err %v after %v", err, time.Since(start))
	}
	if got := readFile(t, p); got["genre"] != "" || got["title"] != "T" {
		t.Errorf("tags %v", got)
	}
}

func TestFetchNoFFmpegForTags(t *testing.T) {
	mp3 := filepath.Join(t.TempDir(), "x.mp3")
	if err := os.WriteFile(mp3, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs(mp3)...)
	if err := a.DownloadTracks(req(t, "original", "lucida", "B00A3SGXAA")); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, a, download.Completed)
	if it := a.GetDownloadState().Items[0]; it.Message != tags.ErrNoFFmpeg.Error() || !strings.HasSuffix(it.Path, ".mp3") {
		t.Fatalf("item = %+v", it)
	}
}

// A tagger that needs a Spotify id gets one for an Amazon track through Amazon's ISRC and Spotify's ISRC search;
// without that search it is skipped and the track still completes.
func TestTaggerNeedsID(t *testing.T) {
	file := testFLAC(t, nil, nil)
	for _, tc := range []struct {
		name      string
		can       []string
		id, genre string
	}{
		{"found", spotifyCan, "US-B00A3SGXAA-1", "Rap"}, // the fake's ISRC is "US-"+id, its ISRC search answers isrc+"-1"
		{"no isrc search", []string{"metadata", "search"}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newPluginApp(t, t.TempDir(), nil,
				srcPlug("spotify", "Spotify", nil, tc.can...), srcPlug("amazon", "Amazon Music", nil, amazonCan...),
				dlPlug("lucida", "Lucida", map[string]any{"copyFrom": file}),
				tagPlug("chosic", "spotify", map[string]any{"tagFields": map[string]any{"genre": "Rap"}}))
			tr := download.Track{ID: "B00A3SGXAA", Title: "T"} // from an Amazon link, with no ISRC
			p, _, err := fetchTrack(t, a, tr, download.Options{Format: "original"})
			if err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, p); got["id_spotify"] != tc.id || got["genre"] != tc.genre {
				t.Errorf("tags %v", got)
			}
		})
	}
}

func TestDisableSourceDependents(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, append(stdPlugs(""), tagPlug("chosic", "spotify", nil))...)
	if err := a.SetPluginOrder([]string{"lucida", "doubledouble"}); err != nil {
		t.Fatal(err)
	}
	if v := view(t, a, "amazon"); !slices.Equal(v.Dependents, []string{"lucida", "doubledouble"}) || v.Blocked != "" {
		t.Fatalf("amazon = %+v", v)
	}
	if v := view(t, a, "spotify"); !slices.Equal(v.Dependents, []string{"chosic"}) {
		t.Fatalf("spotify = %+v", v)
	}
	if v := view(t, a, "lucida"); v.Blocked != "" || len(v.Dependents) != 0 {
		t.Fatalf("lucida = %+v", v)
	}
	if err := a.SetPluginEnabled("amazon", false, false); err != nil {
		t.Fatal(err)
	}
	if v := view(t, a, "lucida"); v.Blocked != "Amazon Music" {
		t.Fatalf("lucida with amazon off = %+v", v)
	}
	if err := a.SetPluginEnabled("chosic", false, false); err != nil {
		t.Fatal(err)
	}
	if v := view(t, a, "spotify"); len(v.Dependents) != 0 {
		t.Fatalf("a disabled tagger is still a dependent: %+v", v)
	}
}

// lucida comes before doubledouble until the user orders the plugins, and a new order reaches the sites at once.
func TestDownloaderOrder(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	idx := func(id string) int {
		return slices.IndexFunc(a.ListPlugins(), func(v PluginView) bool { return v.ID == id })
	}
	if idx("lucida") > idx("doubledouble") {
		t.Fatalf("plugins = %+v", a.ListPlugins())
	}
	if got := a.sources.Names(); !slices.Equal(got, []string{"lucida", "doubledouble"}) {
		t.Fatalf("sites = %v", got)
	}
	if err := a.SetPluginOrder([]string{"doubledouble"}); err != nil {
		t.Fatal(err)
	}
	if got := a.sources.Names(); !slices.Equal(got, []string{"doubledouble", "lucida"}) {
		t.Fatalf("sites after SetPluginOrder = %v", got)
	}
}

func TestTaggerCover(t *testing.T) {
	old, fresh := testPNG(t, 4), testPNG(t, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(fresh) }))
	t.Cleanup(srv.Close)
	a := newPluginApp(t, t.TempDir(), nil, append(stdPlugs(testFLAC(t, tags.Tags{"title": "T"}, old)),
		tagPlug("art", "", map[string]any{"tagFields": map[string]any{"cover": srv.URL}}))...)
	p, _, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "original"})
	if err != nil {
		t.Fatal(err)
	}
	tt, err := readTags(p)
	if err != nil {
		t.Fatal(err)
	}
	if tt.Cover != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(fresh) {
		t.Errorf("cover was not replaced: %.60s", tt.Cover)
	}
	if readFile(t, p)["cover"] != "" {
		t.Error("the cover URL was written as a tag")
	}
}

func TestPluginActionReset(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	dd := func() bool { return site(a, "doubledouble").NeedsAction }
	if err := a.RunPluginAction("doubledouble", "lock"); err != nil { // the fake reports it needs the check
		t.Fatal(err)
	}
	if s := site(a, "doubledouble"); !s.NeedsAction || s.Action != "check" || s.Reason != "DoubleDouble: Log in" {
		t.Fatalf("after lock: %+v", s)
	}
	if err := a.RunPluginAction("doubledouble", "check"); err != nil {
		t.Fatal(err)
	}
	if dd() || !site(a, "doubledouble").Available {
		t.Fatalf("after check: %+v", site(a, "doubledouble"))
	}
	if err := a.RunPluginAction("doubledouble", "nope"); err == nil || err.Error() != "unknown action" {
		t.Fatalf("unknown action: %v", err)
	}
}

func TestPluginActionTimeout(t *testing.T) {
	swap(t, &actionTimeout, 200*time.Millisecond)
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	if err := a.RunPluginAction("doubledouble", "wait"); err == nil || err.Error() != "doubledouble: wait timed out" {
		t.Fatalf("err = %v", err)
	}
}

// Quitting ends a running action, such as a browser check, and with it the plugin.
func TestShutdownEndsRunningAction(t *testing.T) {
	var out syncWriter
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	errc := make(chan error, 1)
	go func() { errc <- a.RunPluginAction("doubledouble", "wait") }()
	waitFor(t, func() bool { return strings.Contains(out.String(), "doubledouble: wait started") })
	a.shutdown(nil)
	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("the action succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the action outlived shutdown")
	}
}

func TestGuardSettings(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	v := view(t, a, "lucida")
	var keys []string
	for _, f := range v.Fields {
		keys = append(keys, f.Key)
	}
	if !slices.Equal(keys, []string{"copyFrom", "$guardLimit", "$guardWait"}) || fmt.Sprint(v.Values["$guardLimit"], v.Values["$guardWait"]) != "50 30" {
		t.Fatalf("lucida fields %q, values %v", keys, v.Values)
	}
	for _, tc := range []struct {
		values map[string]any
		err    string
	}{
		{map[string]any{"$guardLimit": -1.0}, "guard limit must be 0 or more"},
		{map[string]any{"$guardLimit": "x"}, "guard limit must be 0 or more"},
		{map[string]any{"$guardWait": 45.0}, "guard wait must be 15, 30, 60 or 120 minutes"},
	} {
		if err := a.SetPluginSettings("lucida", tc.values); err == nil || err.Error() != tc.err {
			t.Errorf("%v: err = %v", tc.values, err)
		}
	}
	if err := a.SetPluginSettings("lucida", map[string]any{"$guardLimit": 2.0, "$guardWait": 60.0}); err != nil {
		t.Fatal(err)
	}
	if s := site(a, "lucida"); s.Limit != 2 || s.Wait != 60 {
		t.Fatalf("lucida = %+v", s)
	}
	if s := site(a, "doubledouble"); s.Limit != 50 || s.Wait != 30 {
		t.Fatalf("doubledouble = %+v", s)
	}
}

// A switched-off downloader leaves the Balancer's status; a track fixed to it says why it waits.
func TestFixedSourceOffExplained(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs(testFLAC(t, nil, nil))...)
	if err := a.SetPluginEnabled("lucida", false, false); err != nil {
		t.Fatal(err)
	}
	if s := site(a, "lucida"); s.Name != "" {
		t.Fatalf("a switched-off site in the status: %+v", s)
	}
	if err := a.DownloadTracks(req(t, "original", "lucida", "B00A3SGXAA")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		it := a.GetDownloadState().Items
		return len(it) == 1 && it[0].Message == "Lucida is switched off"
	})
	if err := a.SetPluginEnabled("lucida", true, false); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, a, download.Completed)
}

// syncWriter is a log output tests can read while plugins write to it.
type syncWriter struct {
	mu sync.Mutex
	b  strings.Builder
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

func TestSaveSettingsProxyNotifiesPlugins(t *testing.T) {
	var out syncWriter
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr); httpx.SetManualProxy("") })
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	if err := a.RunPluginAction("lucida", "check"); err != nil { // starts it
		t.Fatal(err)
	}
	if err := a.SaveSettings(map[string]any{"theme": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSettings(map[string]any{"proxy": "http://p:1"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(out.String(), "lucida: got settings") })
	if n := strings.Count(out.String(), "lucida: got settings"); n != 1 { // doubledouble starts by itself, so count lucida only
		t.Fatalf("%d settings notes, want 1 (the theme is not the plugins' business)", n)
	}
}

// The download finishes only once the tagger was called, so one after the other would fail with "never appeared".
func TestFetchTaggersOverlapDownload(t *testing.T) {
	called := filepath.Join(t.TempDir(), "called")
	a := newPluginApp(t, t.TempDir(), nil,
		srcPlug("amazon", "Amazon Music", nil, amazonCan...),
		dlPlug("lucida", "Lucida", map[string]any{"copyFrom": testFLAC(t, nil, nil), "waitFor": called}),
		tagPlug("t1", "", map[string]any{"tagFields": map[string]any{"genre": "Pop"}, "touch": called, "tagSleep": "500ms"}))
	p, _, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "original"})
	if err != nil || readFile(t, p)["genre"] != "Pop" {
		t.Fatalf("err %v", err)
	}
}

func TestFetchCancelsTaggersOnReturn(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil,
		srcPlug("amazon", "Amazon Music", nil, amazonCan...),
		dlPlug("lucida", "Lucida", map[string]any{"fail": "boom"}),
		tagPlug("slow", "", map[string]any{"tagFields": map[string]any{"genre": "Pop"}, "tagSleep": "30s"}))
	start := time.Now()
	if _, _, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "original"}); err == nil || !strings.Contains(err.Error(), "fake: boom") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("fetch waited %v for the tagger", d)
	}
}

func TestFetchTaggerDisabled(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, append(stdPlugs(testFLAC(t, nil, nil)),
		tagPlug("t1", "", map[string]any{"tagFields": map[string]any{"genre": "Pop"}, "tagSleep": "300ms"}))...)
	if err := a.SetPluginEnabled("t1", false, false); err != nil {
		t.Fatal(err)
	}
	p, st, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "original"})
	if err != nil || readFile(t, p)["genre"] != "" || slices.Contains(st, "Looking up tags…") {
		t.Errorf("err %v, statuses %q", err, st)
	}
}

// Tags go into the converted file, after the conversion.
func TestFetchConvertTagsTheOutput(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, append(stdPlugs(testFLAC(t, nil, nil)),
		tagPlug("t1", "", map[string]any{"tagFields": map[string]any{"genre": "Rap"}}))...)
	a.findFFmpeg = func(string) string { return "ff" }
	var in string
	swap(t, &convertFile, func(_ context.Context, _, from, _ string) (string, error) {
		in = from
		out := strings.TrimSuffix(from, ".flac") + "-converted.flac" // a FLAC stands in for the M4A, so the tags need no FFmpeg
		b, err := os.ReadFile(from)
		if err != nil {
			return "", err
		}
		return out, os.WriteFile(out, b, 0o644)
	})
	p, st, err := fetchTrack(t, a, amazonTrack, download.Options{Format: "m4a-aac-320"})
	if err != nil || !strings.HasSuffix(p, "-converted.flac") || !slices.Contains(st, "Converting to M4A…") {
		t.Fatalf("path %q, err %v, statuses %q", p, err, st)
	}
	if readFile(t, p)["genre"] != "Rap" || readFile(t, in)["genre"] != "" {
		t.Errorf("out %v, in %v", readFile(t, p), readFile(t, in))
	}
}

// A plugin already installed answers "exists" and an import of it "conflict:<id>"; the retry reuses the remembered zip.
func TestInstallAndImportAnswers(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil)
	zp := filepath.Join(t.TempDir(), "extra.zip")
	f, err := os.Create(zp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, _ := zw.Create("plugin.json")
	w.Write([]byte(`{"id":"extra","name":"Extra","version":"1.0.0","kind":"tool","run":["extra.exe"]}`))
	if err := errors.Join(zw.Close(), f.Close()); err != nil {
		t.Fatal(err)
	}
	if id, err := a.installFrom(zp, false); id != "extra" || err != nil {
		t.Fatalf("install: %q, %v", id, err)
	}
	if _, err := a.installFrom(zp, false); err == nil || err.Error() != "exists" || a.installZip != zp {
		t.Fatalf("again: %v, kept %q", err, a.installZip)
	}
	if id, err := a.installFrom(zp, true); id != "extra" || err != nil || a.installZip != "" {
		t.Fatalf("replace: %q, %v, kept %q", id, err, a.installZip)
	}

	exp := filepath.Join(t.TempDir(), "export.zip")
	if err := a.reg.Export(exp); err != nil {
		t.Fatal(err)
	}
	if _, err := a.importFrom(exp, ""); err == nil || err.Error() != "conflict:extra" || a.importZip != exp {
		t.Fatalf("import: %v, kept %q", err, a.importZip)
	}
	if need, err := a.importFrom(exp, "skip"); err != nil || need == nil || a.importZip != "" {
		t.Fatalf("skip: %v, %v, kept %q", need, err, a.importZip)
	}
}

// An import that fails part-way names the plugins it installed before the error: "installed:<id,id>:<error>".
func TestImportPartialNamesInstalled(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil)
	for _, id := range []string{"aa", "bb"} {
		zp := filepath.Join(t.TempDir(), id+".zip")
		f, err := os.Create(zp)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		w, _ := zw.Create("plugin.json")
		w.Write([]byte(`{"id":"` + id + `","name":"X","version":"1.0.0","kind":"tool","run":["x.exe"]}`))
		if err := errors.Join(zw.Close(), f.Close()); err != nil {
			t.Fatal(err)
		}
		if _, err := a.installFrom(zp, false); err != nil {
			t.Fatal(err)
		}
	}
	exp := filepath.Join(t.TempDir(), "export.zip")
	if err := a.reg.Export(exp); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"aa", "bb"} {
		if err := a.RemovePlugin(id); err != nil {
			t.Fatal(err)
		}
	}
	// Another plugin's folder is named bb, so bb's own folder cannot be moved in.
	if err := os.MkdirAll(filepath.Join(a.pluginsDir, "bb"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a.pluginsDir, "bb", "plugin.json"), []byte(`{"id":"zz","name":"Z","version":"1.0.0","kind":"tool","run":["z.exe"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := a.importFrom(exp, "")
	if err == nil || !strings.HasPrefix(err.Error(), `installed:aa:plugin "bb": `) {
		t.Fatalf("import: %v", err)
	}
}

// With no token doubledouble says from Init that it needs its check. The App starts the enabled downloaders at
// startup, so the Settings line shows, and a download fixed to it is refused, before any track reaches it.
func TestFirstRunCheckShowsAtStartup(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, srcPlug("amazon", "Amazon Music", nil, amazonCan...),
		dlPlug("doubledouble", "DoubleDouble", map[string]any{"initNeeds": "Needs the browser check"}))
	dd := func() source.Status {
		for _, s := range a.GetSourceStatus() {
			if s.Name == "doubledouble" {
				return s
			}
		}
		return source.Status{}
	}
	waitFor(t, func() bool { return dd().NeedsAction })
	if s := dd(); s.Action != "check" || s.Reason != "DoubleDouble: Needs the browser check" {
		t.Fatalf("status %+v", s)
	}
	if err := a.DownloadTracks(req(t, "", "doubledouble", "B00A3SGXAA")); err == nil || err.Error() != "DoubleDouble: Needs the browser check" {
		t.Fatalf("err = %v", err)
	}
}

// A hung source plugin cannot hang the search box: the call gives up after sourceTimeout.
func TestSearchTimesOut(t *testing.T) {
	swap(t, &sourceTimeout, 500*time.Millisecond)
	a := newPluginApp(t, t.TempDir(), nil, srcPlug("spotify", "Spotify", map[string]any{"searchSleep": "30s"}, spotifyCan...))
	start := time.Now()
	if _, err := a.Search("spotify", "q", 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("Search took %v", d)
	}
}
