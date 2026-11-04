package main

import (
	"os"
	"testing"

	"sonar/internal/convert"
	"sonar/internal/history"
	"sonar/internal/httpx"
)

func TestGetMetadataUnsupported(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil, stdPlugs("")...)
	for _, link := range []string{"https://example.com/x", "hello"} {
		if _, err := a.GetMetadata(link); err == nil || err.Error() != "Unsupported link. Paste a link from Amazon Music or Spotify." {
			t.Fatalf("%s: err = %v", link, err)
		}
	}
	a = newPluginApp(t, t.TempDir(), nil)
	if _, err := a.GetMetadata("https://spotify/x"); err == nil || err.Error() != "Unsupported link: no source plugin is turned on." {
		t.Fatalf("no sources: err = %v", err)
	}
}

// An exe-only upgrade leaves no plugins folder next to sonar.exe: say so instead of "no source plugin is turned on".
func TestBuiltinPluginsMissing(t *testing.T) {
	a := newPluginApp(t, t.TempDir(), nil)
	if err := os.Remove(a.builtinDir); err != nil {
		t.Fatal(err)
	}
	want := "no_builtin_plugins:" + a.builtinDir
	if _, err := a.GetMetadata("https://spotify/x"); err == nil || err.Error() != want {
		t.Fatalf("GetMetadata: err = %v", err)
	}
	if err := a.DownloadTracks(req(t, "", "", "B00A3SGXAA")); err == nil || err.Error() != want {
		t.Fatalf("DownloadTracks: err = %v", err)
	}
}

func TestSearchValidates(t *testing.T) {
	a := &App{}
	if _, err := a.Search("spotify", "", 0); err == nil || err.Error() != "search query is required" {
		t.Fatalf("Search: err = %v", err)
	}
	if _, err := a.SearchByType("spotify", "", "album", 0, 0); err == nil || err.Error() != "search query is required" {
		t.Fatalf("SearchByType: err = %v", err)
	}
	if _, err := a.SearchByType("spotify", "q", "", 0, 0); err == nil || err.Error() != "search type is required" {
		t.Fatalf("SearchByType: err = %v", err)
	}
}

func TestSaveSettingsRejectsBadProxy(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	t.Cleanup(func() { httpx.SetManualProxy("") })
	if err := a.SaveSettings(map[string]any{"proxy": "http://ok:1"}); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveSettings(map[string]any{"proxy": "ftp://x:1", "theme": "x"}); err == nil {
		t.Fatal("want error for ftp proxy")
	}
	s, err := a.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s["proxy"] != "http://ok:1" || s["theme"] != nil {
		t.Errorf("failed save changed config: %v", s)
	}
}

func TestHistoryUnavailable(t *testing.T) {
	a := &App{dataDir: t.TempDir()}
	if _, err := a.GetFetchHistory(); err == nil || err.Error() != "history is unavailable" {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadSettingsEmptyIsNotNil(t *testing.T) {
	s, err := (&App{dataDir: t.TempDir()}).LoadSettings()
	if err != nil || s == nil || len(s) != 0 {
		t.Fatalf("got %#v, %v; want empty non-nil map", s, err)
	}
}

func TestInitOpensHistoryAndAppliesSavedProxy(t *testing.T) {
	user := t.TempDir()
	t.Setenv("LOCALAPPDATA", user)
	dir := t.TempDir()
	t.Cleanup(func() { httpx.SetManualProxy("") })
	if err := (&App{dataDir: dir}).SaveSettings(map[string]any{"proxy": "http://saved:9", "language": "ru"}); err != nil {
		t.Fatal(err)
	}
	httpx.SetManualProxy("")
	a := &App{dataDir: dir}
	a.init()
	t.Cleanup(func() { a.shutdown(nil) })
	if a.hist == nil {
		t.Fatal("history not opened")
	}
	if got := httpx.ManualProxy(); got != "http://saved:9" {
		t.Fatalf("manual proxy = %q", got)
	}
	if _, want := pluginDirs(convert.ExeDir(), dir); a.pluginsDir != want {
		t.Fatalf("plugins dir = %q", a.pluginsDir)
	}
	if p := a.pluginInit("x"); p.Lang != "ru" || p.Proxy != "http://saved:9" || p.EffectiveProxy != "http://saved:9" {
		t.Fatalf("init params = %+v", p)
	}
	if err := a.AddFetchHistory(history.FetchItem{Type: "track", URL: "u", Data: "{}"}); err != nil {
		t.Fatal(err)
	}
	if items, _ := a.GetFetchHistory(); len(items) != 1 || items[0].Data != "" {
		t.Fatalf("list = %+v", items)
	}
	if d, err := a.GetFetchHistoryData("track|u"); err != nil || d != "{}" {
		t.Fatalf("data = %q, %v", d, err)
	}
	if _, err := a.GetFetchHistoryData("track|nope"); err == nil {
		t.Fatal("want error for unknown id")
	}
}

