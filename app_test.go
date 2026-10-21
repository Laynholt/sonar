package main

import (
	"strings"
	"testing"

	"sonar/internal/httpx"
)

func TestGetMetadataRejectsAmazonLink(t *testing.T) {
	_, err := (&App{}).GetMetadata("https://music.amazon.com/albums/B01DPROTRO")
	if err == nil || !strings.Contains(err.Error(), "not supported yet") {
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
