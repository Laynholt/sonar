package plugins_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	if len(built) != 2 {
		t.Fatalf("built %v, want spotify and amazon", built)
	}
	for id := range built {
		m := manifest(t, id)
		if m.ID != id || m.Kind != plugin.KindSource || m.Source.Service != id || m.Contract != plugin.Contract {
			t.Errorf("%s: %+v", id, m)
		}
	}
}

// start runs the built plugin through a Host, the way sonar will.
func start(t *testing.T, id string) *plugin.Host {
	t.Helper()
	dir := t.TempDir()
	h := plugin.NewHost(plugin.Found{M: manifest(t, id), Dir: built[id]}, func() plugin.InitParams {
		return plugin.InitParams{Contract: plugin.Contract, Lang: "en", Settings: map[string]any{}, DataDir: dir}
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
