package resolve

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sonar/internal/plugin"
)

func TestPluginFindersMissingSource(t *testing.T) {
	root := t.TempDir()
	reg := plugin.OpenRegistry(filepath.Join(root, "data"), filepath.Join(root, "b"), filepath.Join(root, "u"), nil,
		func(string) plugin.InitParams { return plugin.InitParams{} }, nil)
	t.Cleanup(reg.Close)
	for _, p := range [][2]string{{"deezer", "amazon"}, {"deezer", "tidal"}} {
		_, err := PluginFinders(reg)(p[0], p[1])
		if err == nil || !strings.Contains(err.Error(), p[0]) {
			t.Errorf("%v: err = %v, want one naming %s", p, err, p[0])
		}
	}
}

func TestPair(t *testing.T) {
	src := func(service string, can ...string) *plugin.SourceClient {
		return &plugin.SourceClient{ID: service, Service: service, Can: can}
	}
	origin := src("spotify", "isrc")
	for _, c := range []struct {
		name   string
		target *plugin.SourceClient
		bad    string // the capability the error names, or ""
	}{
		{"no find", src("amazon", "tracks", "album"), "find"},
		{"no tracks", src("amazon", "find", "album"), "tracks"},
	} {
		if _, err := pair(origin, c.target); err == nil || !strings.Contains(err.Error(), c.bad) || !strings.Contains(err.Error(), "amazon") {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}

	ctx := context.Background()
	f, err := pair(src("spotify"), src("amazon", "find", "tracks"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.LookupAlbum(ctx, "A"); !errors.Is(err, plugin.ErrCannot) {
		t.Errorf("LookupAlbum err = %v", err)
	}
	if _, err := f.OriginIDsByISRC(ctx, "X"); !errors.Is(err, plugin.ErrCannot) {
		t.Errorf("OriginIDsByISRC err = %v", err)
	}
	if _, err := pair(origin, src("amazon", "find", "tracks", "album")); err != nil {
		t.Errorf("full pair: %v", err)
	}
}
