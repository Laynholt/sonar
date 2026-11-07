package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sonar/internal/amazon"
	"sonar/internal/model"
	"sonar/internal/plugin"
	"sonar/internal/spotify"
)

// built maps a plugin id to the folder holding its freshly built program and plugin.json.
var built = map[string]string{}

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "sonar-builtin-plugins")
	if err != nil {
		panic(err)
	}
	entries, _ := os.ReadDir(".")
	for _, e := range entries {
		if _, err := os.Stat(filepath.Join(e.Name(), "plugin.json")); err != nil {
			continue
		}
		id, dir := e.Name(), filepath.Join(tmp, e.Name())
		exe := id
		if runtime.GOOS == "windows" {
			exe += ".exe"
		}
		if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, exe), "./"+id).CombinedOutput(); err != nil {
			panic(fmt.Sprintf("build %s: %v\n%s", id, err, out))
		}
		raw, err := os.ReadFile(filepath.Join(id, "plugin.json"))
		if err != nil {
			panic(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "plugin.json"), raw, 0o644); err != nil {
			panic(err)
		}
		built[id] = dir
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

func manifest(t *testing.T, id string) plugin.Manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(built[id], "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugin.ParseManifest(raw)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return m
}

func TestBuiltinManifests(t *testing.T) {
	for _, id := range []string{"spotify", "amazon"} {
		if built[id] == "" {
			t.Errorf("%s was not built; built: %v", id, built)
		} else if m := manifest(t, id); m.Kind != plugin.KindSource || m.Source.Service != id {
			t.Errorf("%s: %+v", id, m)
		}
	}
	for _, id := range []string{"lucida", "doubledouble"} {
		if built[id] == "" {
			t.Errorf("%s was not built; built: %v", id, built)
		} else if m := manifest(t, id); m.Kind != plugin.KindDownloader || m.Downloader.Accepts != "amazon" || m.Downloader.Guard.Limit != 50 {
			t.Errorf("%s: %+v", id, m)
		}
	}
	if built["chosic"] == "" {
		t.Errorf("chosic was not built; built: %v", built)
	} else if m := manifest(t, "chosic"); m.Kind != plugin.KindTagger || len(m.Tagger.Fields) != 1 || m.Tagger.Fields[0] != "genre" || m.Tagger.Needs != "spotify" {
		t.Errorf("chosic: %+v", m)
	}
	for id := range built {
		if m := manifest(t, id); m.ID != id || m.Contract != plugin.Contract {
			t.Errorf("%s: %+v", id, m)
		}
	}
}

// Every method a source's manifest lists in can must be registered, and nothing else may be:
// bad params make a registered method fail before any network, an unknown one answers -32601.
func TestSourceCanMatchesHandlers(t *testing.T) {
	for id := range built {
		m := manifest(t, id)
		if m.Kind != plugin.KindSource {
			continue
		}
		h := start(t, id)
		call := func(method string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return h.Call(ctx, method, json.RawMessage(`"bad"`), nil, nil)
		}
		for _, c := range m.Source.Can {
			var pe *plugin.Error
			if err := call("source." + c); !errors.As(err, &pe) || pe.Code == -32601 {
				t.Errorf("%s: can %q: want a registered method, got %v", id, c, err)
			}
		}
		var pe *plugin.Error
		if err := call("source.nope"); !errors.As(err, &pe) || pe.Code != -32601 {
			t.Errorf("%s: source.nope: want -32601, got %v", id, err)
		}
	}
}

// start runs the built plugin through a Host, the way sonar will.
func start(t *testing.T, id string) *plugin.Host {
	t.Helper()
	return startWith(t, id, t.TempDir(), map[string]any{})
}

// startWith is start with a chosen data dir and settings.
func startWith(t *testing.T, id, dir string, settings map[string]any) *plugin.Host {
	t.Helper()
	h := plugin.NewHost(plugin.Found{M: manifest(t, id), Dir: built[id]}, func() plugin.InitParams {
		return plugin.InitParams{Contract: plugin.Contract, Lang: "en", Settings: settings, DataDir: dir}
	}, t.Logf)
	t.Cleanup(h.Close)
	return h
}

func parseLink(h *plugin.Host, url string) (plugin.Link, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var l plugin.Link
	err := h.Call(ctx, plugin.MSourceParseLink, map[string]string{"url": url}, &l, nil)
	return l, err
}

func checkParse(t *testing.T, id string, good map[string]plugin.Link, bad string) {
	h := start(t, id)
	for url, want := range good {
		if got, err := parseLink(h, url); err != nil || got != want {
			t.Errorf("%s: got %+v, %v; want %+v", url, got, err, want)
		}
	}
	if _, err := parseLink(h, bad); err == nil {
		t.Errorf("%s: want an error", bad)
	}
}

func TestSpotifyPluginParseLink(t *testing.T) {
	checkParse(t, "spotify", map[string]plugin.Link{
		"https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8?si=x":             {Kind: "track", ID: "4PTG3Z6ehGkBFwjybzWkR8"},
		"spotify:album:6N9PS4QXF1D0OWPk0Sxtb4":                                   {Kind: "album", ID: "6N9PS4QXF1D0OWPk0Sxtb4"},
		"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt/discography/all": {Kind: "artist", ID: "0gxyHStUsqpMadRV0Di1Qt"},
		"https://open.spotify.com/intl-de/playlist/37i9dQZF1DXcBWIGoYBM5M":       {Kind: "playlist", ID: "37i9dQZF1DXcBWIGoYBM5M"},
	}, "https://open.spotify.com/track/short")
}

func TestAmazonPluginParseLink(t *testing.T) {
	checkParse(t, "amazon", map[string]plugin.Link{
		"https://music.amazon.com/albums/B00A3SGWYC":                      {Kind: "album", ID: "B00A3SGWYC"},
		"music.amazon.co.uk/artists/B06WP75FR9/olivia-rodrigo":            {Kind: "artist", ID: "B06WP75FR9"},
		"https://music.amazon.com/albums/B00A3SGWYC?trackAsin=B00A3SGXAA": {Kind: "track", ID: "B00A3SGXAA"},
	}, "https://music.amazon.com/user-playlists/B00A3SGXAA")
}

// The host anchors source.links as ^(?:pat)$ over the whole pasted URL; they must say what IsLink says.
func TestLinkPatternsAgreeWithIsLink(t *testing.T) {
	samples := []string{
		"", "   ", "https://open.spotify.com/track/short",
		"https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8",
		"http://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8",
		"  https://open.spotify.com/intl-de/album/6N9PS4QXF1D0OWPk0Sxtb4?si=abc  ",
		"spotify:album:6N9PS4QXF1D0OWPk0Sxtb4",
		"spotify:track:4PTG3Z6ehGkBFwjybzWkR8 ",
		"spotify:show:6N9PS4QXF1D0OWPk0Sxtb4",
		"spotify:album:short",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M",
		"https://open.spotify.com/embed/track/4PTG3Z6ehGkBFwjybzWkR8",
		"https://open.spotify.com/embed/intl-fr/track/4PTG3Z6ehGkBFwjybzWkR8",
		"https://play.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8",
		"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt",
		"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt/",
		"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt/discography/all",
		"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt/related",
		"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt?si=1#frag",
		"https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8/extra",
		"https://open.spotify.com/show/4PTG3Z6ehGkBFwjybzWkR8",
		"https://open.spotify.com/",
		"https://open.spotify.com:443/track/4PTG3Z6ehGkBFwjybzWkR8",
		"https://example.com/track/4PTG3Z6ehGkBFwjybzWkR8",
		"open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8",
		"https://music.amazon.com/albums/B00A3SGWYC",
		"https://music.amazon.co.uk/artists/B06WP75FR9/olivia-rodrigo",
		"https://music.amazon.de/tracks/B00A3SGXAA?marketplaceId=A1PA6795UKMFR9&musicTerritory=DE",
		"https://www.music.amazon.com/albums/B00A3SGWYC",
		"https://MUSIC.AMAZON.COM/albums/B00A3SGWYC",
		"  music.amazon.com/albums/B00A3SGWYC  ",
		"music.amazon.com",
		"https://music.amazon.com",
		"https://music.amazon.com:8443/albums/B00A3SGWYC",
		"https://music.amazon.com?x=1",
		"https://www.amazon.com/dp/B00A3SGWYC",
		"https://music.amazonaws.com/albums/B00A3SGWYC",
		"https://evilmusic.amazon.com/albums/B00A3SGWYC",
		"https://open.spotify.com/track/x",
	}
	for _, c := range []struct {
		id string
		is func(string) bool
	}{{"spotify", spotify.IsLink}, {"amazon", amazon.IsLink}} {
		m := manifest(t, c.id)
		var res []*regexp.Regexp
		for _, p := range m.Source.Links {
			res = append(res, regexp.MustCompile(`^(?:`+p+`)$`))
		}
		if len(res) == 0 {
			t.Fatalf("%s: no links", c.id)
		}
		for _, s := range samples {
			got := false
			for _, re := range res {
				got = got || re.MatchString(s)
			}
			if want := c.is(s); got != want {
				t.Errorf("%s: patterns say %v, IsLink says %v for %q", c.id, got, want, s)
			}
		}
	}
}

// TestLiveSourcePlugins asks the real plugins for one Spotify track and one Amazon album.
func TestLiveSourcePlugins(t *testing.T) {
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1 to hit Spotify and Amazon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	meta := func(id, url string, into any) {
		t.Helper()
		var raw json.RawMessage
		if err := start(t, id).Call(ctx, plugin.MSourceMetadata, map[string]string{"url": url}, &raw, nil); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	var tr model.TrackResponse
	meta("spotify", "https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8", &tr)
	if tr.Track.Name == "" {
		t.Errorf("spotify track: %+v", tr)
	}
	var al model.AlbumResponsePayload
	meta("amazon", "https://music.amazon.com/albums/B00A3SGWYC", &al)
	if al.AlbumInfo.Name == "" {
		t.Errorf("amazon album: %+v", al)
	}
}

const trackID = "B00A3SGXAA"

// fakeLucida serves lucida.to's flow for the one track in the lucida test fixture; with status != 0 every answer is that status.
func fakeLucida(t *testing.T, status int) {
	t.Helper()
	page, err := os.ReadFile("../../internal/lucida/testdata/data_track.json")
	if err != nil {
		t.Fatal(err)
	}
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		switch r.URL.Query().Get("url") {
		case "/api/fetch/stream/v2":
			w.Write([]byte(`{"success":true,"handoff":"h1","name":"srv"}`))
		case "/api/fetch/request/h1":
			if polls++; polls == 1 {
				w.Write([]byte(`{"success":true,"status":"ripping","message":"Ripping {item}..."}`))
			} else {
				w.Write([]byte(`{"success":true,"status":"completed"}`))
			}
		case "/api/fetch/request/h1/download":
			w.Header().Set("Content-Type", "audio/flac")
			w.Write([]byte("fLaC-test"))
		default:
			w.Write(page)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SONAR_LUCIDA_BASE", srv.URL)
}

func download(h *plugin.Host, pathNoExt string, onProgress func(plugin.Progress)) (plugin.DownloadResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var res plugin.DownloadResult
	err := h.Call(ctx, plugin.MDownload, plugin.DownloadParams{IDs: map[string]string{"amazon": trackID}, PathNoExt: pathNoExt}, &res, onProgress)
	return res, err
}

func TestLucidaPluginDownload(t *testing.T) {
	fakeLucida(t, 0)
	base := filepath.Join(t.TempDir(), "a", "song")
	var notes []plugin.Progress
	res, err := download(start(t, "lucida"), base, func(p plugin.Progress) { notes = append(notes, p) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != base+".flac" {
		t.Fatalf("path %q", res.Path)
	}
	if b, _ := os.ReadFile(res.Path); string(b) != "fLaC-test" {
		t.Fatalf("content %q", b)
	}
	var status, bytes bool
	for _, n := range notes {
		status = status || n.Status == "Ripping track..."
		bytes = bytes || n.Bytes == int64(len("fLaC-test"))
	}
	if !status || !bytes {
		t.Fatalf("want a status note and a byte note, got %+v", notes)
	}
}

func TestDownloaderUnavailable(t *testing.T) {
	fakeLucida(t, http.StatusServiceUnavailable)
	_, err := download(start(t, "lucida"), filepath.Join(t.TempDir(), "x"), nil)
	var pe *plugin.Error
	if !errors.As(err, &pe) || pe.Code != plugin.CodeUnavailable {
		t.Fatalf("want -32001, got %v", err)
	}
}

func TestDoubleDoubleNeedsAction(t *testing.T) {
	t.Setenv("SONAR_DD_BASE", "http://127.0.0.1:1") // no token and no auto-solver: the check must be asked for at once
	h := start(t, "doubledouble")
	var mu sync.Mutex
	var statuses []plugin.PluginStatus
	h.OnStatus = func(st plugin.PluginStatus) {
		mu.Lock()
		statuses = append(statuses, st)
		mu.Unlock()
	}
	_, err := download(h, filepath.Join(t.TempDir(), "x"), nil)
	var pe *plugin.Error
	if !errors.As(err, &pe) || pe.Code != plugin.CodeNeedsAction {
		t.Fatalf("want -32002, got %v", err)
	}
	var na plugin.NeedsAction
	if json.Unmarshal(pe.Data, &na) != nil || na.Action != "check" {
		t.Errorf("error data %s", pe.Data)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(statuses) != 1 || statuses[0].Available || statuses[0].NeedsAction == nil || statuses[0].NeedsAction.Action != "check" {
		t.Fatalf("statuses %+v", statuses)
	}
	const hint = "doubledouble needs a CAPTCHA token"
	if !strings.HasPrefix(statuses[0].NeedsAction.Message, hint) || !strings.HasPrefix(na.Message, hint) {
		t.Errorf("status %q, error data %q", statuses[0].NeedsAction.Message, na.Message)
	}
}

// The check's instructions follow the user's language.
func TestDoubleDoubleHintInRussian(t *testing.T) {
	t.Setenv("SONAR_DD_BASE", "http://127.0.0.1:1")
	h := plugin.NewHost(plugin.Found{M: manifest(t, "doubledouble"), Dir: built["doubledouble"]}, func() plugin.InitParams {
		return plugin.InitParams{Contract: plugin.Contract, Lang: "ru", Settings: map[string]any{}, DataDir: t.TempDir()}
	}, t.Logf)
	t.Cleanup(h.Close)
	_, err := download(h, filepath.Join(t.TempDir(), "x"), nil)
	var pe *plugin.Error
	var na plugin.NeedsAction
	if !errors.As(err, &pe) || json.Unmarshal(pe.Data, &na) != nil || !strings.HasPrefix(na.Message, "doubledouble нужен CAPTCHA-токен") {
		t.Fatalf("err %v, data %s", err, pe.Data)
	}
}

func TestDoubleDoubleUnknownAction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := start(t, "doubledouble").Call(ctx, plugin.MAction, map[string]string{"name": "nope"}, nil, nil)
	var pe *plugin.Error
	if !errors.As(err, &pe) || pe.Code != plugin.CodeOther || pe.Message != "unknown action" {
		t.Fatalf("got %v", err)
	}
}

// fakeChosic serves chosic's genre-finder API for track t1 (artist a1) and counts the requests for the artist.
func fakeChosic(t *testing.T) *atomic.Int32 {
	t.Helper()
	var artistHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tools/handshake/":
			w.Write([]byte(`{"success":true}`))
		case "/api/tools/tracks/t1":
			w.Write([]byte(`{"artists":[{"id":"a1"}]}`))
		case "/api/tools/artists/a1":
			artistHits.Add(1)
			w.Write([]byte(`{"genres":["russian hip hop","pop","rap","trap","drill","grime"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("SONAR_CHOSIC_BASE", srv.URL)
	return &artistHits
}

func tag(h *plugin.Host, ids map[string]string) (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var res plugin.TagResult
	err := h.Call(ctx, plugin.MTag, plugin.TagParams{IDs: ids}, &res, nil)
	return res.Fields, err
}

func TestChosicPluginTag(t *testing.T) {
	hits := fakeChosic(t)
	dir := t.TempDir()
	ids := map[string]string{"spotify": "t1"}
	h := startWith(t, "chosic", dir, map[string]any{})
	for i := 0; i < 2; i++ { // the second answer comes from the cache
		if got, err := tag(h, ids); err != nil || len(got) != 1 || got["genre"] != "Russian Hip Hop, Pop, Rap, Trap, Drill" {
			t.Fatalf("call %d: %v, %v", i, got, err)
		}
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("chosic asked %d times, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dir, "genres.json")); err != nil {
		t.Errorf("cache file: %v", err)
	}

	// singleGenre at start, and a $/settings note flips it without a restart (the genres are cached, so no new hit).
	one := startWith(t, "chosic", t.TempDir(), map[string]any{"singleGenre": true})
	if got, err := tag(one, ids); err != nil || got["genre"] != "Russian Hip Hop" {
		t.Fatalf("single: %v, %v", got, err)
	}
	if err := one.Notify(plugin.MSettingsChanged, plugin.InitParams{Settings: map[string]any{"singleGenre": false}}); err != nil {
		t.Fatal(err)
	}
	if got, err := tag(one, ids); err != nil || got["genre"] != "Russian Hip Hop, Pop, Rap, Trap, Drill" {
		t.Fatalf("after the settings note: %v, %v", got, err)
	}
}

func TestChosicPluginNoID(t *testing.T) {
	hits := fakeChosic(t)
	for _, ids := range []map[string]string{nil, {"amazon": "B00A3SGXAA"}} {
		if got, err := tag(start(t, "chosic"), ids); err != nil || len(got) != 0 {
			t.Errorf("ids %v: got %v, %v; want no fields and no error", ids, got, err)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("chosic asked %d times without an id", n)
	}
}

// TestLiveChosicPlugin asks the real chosic.com for the genres of one Spotify track.
func TestLiveChosicPlugin(t *testing.T) {
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1 to hit chosic.com")
	}
	got, err := tag(start(t, "chosic"), map[string]string{"spotify": "0VjIjW4GlUZAMYd2vXMi3b"})
	if err != nil || got["genre"] == "" {
		t.Fatalf("got %v, %v", got, err)
	}
	t.Logf("genre: %q", got["genre"])
}
