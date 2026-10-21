package main

import (
	"strings"
	"testing"

	"sonar/internal/history"
	"sonar/internal/httpx"
)

func TestGetMetadataRejectsCommunityPlaylist(t *testing.T) {
	_, err := (&App{}).GetMetadata("https://music.amazon.com/user-playlists/f02fa03d3e404c428e53d4cfb58ce93bc2a0")
	if err == nil || !strings.Contains(err.Error(), "need an account") {
		t.Fatalf("err = %v", err)
	}
}

func TestGetMetadataUnsupported(t *testing.T) {
	_, err := (&App{}).GetMetadata("https://example.com/x")
	if err == nil || err.Error() != "Unsupported link. Paste a Spotify or Amazon Music link." {
		t.Fatalf("err = %v", err)
	}
}

func TestGetPreviewURLAmazonID(t *testing.T) {
	if u, err := (&App{}).GetPreviewURL("B00A3SGXAA"); u != "" || err != nil {
		t.Fatalf("got %q, %v; want empty, nil", u, err)
	}
}

func TestSearchAmazonEmptyQuery(t *testing.T) {
	_, err := (&App{}).SearchAmazon(AmazonSearchRequest{})
	if err == nil || err.Error() != "search query is required" {
		t.Fatalf("err = %v", err)
	}
}

func TestGetMetadataRejectsGarbage(t *testing.T) {
	_, err := (&App{}).GetMetadata("hello")
	if err == nil || !strings.Contains(err.Error(), "Unsupported link") {
		t.Fatalf("err = %v", err)
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
	dir := t.TempDir()
	t.Cleanup(func() { httpx.SetManualProxy("") })
	if err := (&App{dataDir: dir}).SaveSettings(map[string]any{"proxy": "http://saved:9"}); err != nil {
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
