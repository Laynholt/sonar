package doubledouble

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
)

const origin = "https://eu.doubledouble.top/"

func utf16le(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = append(b, byte(u), byte(u>>8))
	}
	return b
}

// writeSessionStorage lays out sessionStorage the way Chromium keeps it on disk.
func writeSessionStorage(t *testing.T, profileDir string, kv map[string][]byte) {
	t.Helper()
	db, err := leveldb.OpenFile(filepath.Join(profileDir, "Default", "Session Storage"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for k, v := range kv {
		if err := db.Put([]byte(k), v, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadTokenPicksTheSiteTab(t *testing.T) {
	dir := t.TempDir()
	if got := readToken(dir, origin); got != "" { // no storage yet
		t.Fatalf("got %q", got)
	}
	writeSessionStorage(t, dir, map[string][]byte{
		"namespace-aaa-https://other.example/": []byte("0"),
		"map-0-captchaToken":                   utf16le("wrong"),
		"namespace-bbb-" + origin:              []byte("1"),
		"map-1-captcha-id":                     utf16le("x"),
		"map-1-captchaToken":                   utf16le("EjM7VUY"),
	})
	if got := readToken(dir, origin); got != "EjM7VUY" {
		t.Fatalf("got %q", got)
	}
}

func TestWaitTokenReadsSavedToken(t *testing.T) {
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = time.Second })
	dir := t.TempDir()
	c := New(filepath.Join(t.TempDir(), "token"))
	c.Base = strings.TrimSuffix(origin, "/")
	time.AfterFunc(30*time.Millisecond, func() {
		writeSessionStorage(t, dir, map[string][]byte{"namespace-x-" + origin: []byte("0"), "map-0-captchaToken": utf16le("tok")})
	})
	tok, err := c.waitToken(context.Background(), dir, make(chan struct{}))
	if err != nil || tok != "tok" {
		t.Fatalf("got %q %v", tok, err)
	}
}

func TestWaitTokenWindowClosedOrCancelled(t *testing.T) {
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = time.Second })
	c := New(filepath.Join(t.TempDir(), "token"))
	exited := make(chan struct{})
	time.AfterFunc(20*time.Millisecond, func() { close(exited) })
	if _, err := c.waitToken(context.Background(), t.TempDir(), exited); !errors.Is(err, ErrCheckClosed) {
		t.Fatalf("closed: got %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := c.waitToken(ctx, t.TempDir(), make(chan struct{})); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: got %v", err)
	}
}

func TestCheckRejectsSecondCheck(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "token"))
	c.checking.Store(true)
	err := c.Check(context.Background(), "no-such-browser", t.TempDir(), trackURL)
	if err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("got %v", err)
	}
}

func TestPrepareProfile(t *testing.T) {
	dir := t.TempDir()
	def := filepath.Join(dir, "Default")
	for _, sub := range []string{"Sessions", "Session Storage"} {
		os.MkdirAll(filepath.Join(def, sub), 0o755)
		os.WriteFile(filepath.Join(def, sub, "x"), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(def, "Cookies"), []byte("keep"), 0o644)
	os.WriteFile(filepath.Join(def, "Preferences"), []byte(`{"keep":1,"profile":{"exit_type":"Crashed","name":"p"}}`), 0o644)

	if err := prepareProfile(dir, `C:\dl`); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"Sessions", "Session Storage"} {
		if _, err := os.Stat(filepath.Join(def, sub)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s left: %v", sub, err)
		}
	}
	if _, err := os.Stat(filepath.Join(def, "Cookies")); err != nil {
		t.Fatalf("cookies must stay: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(def, "Preferences"))
	var p struct {
		Keep     int
		Download struct {
			DefaultDirectory  string `json:"default_directory"`
			PromptForDownload bool   `json:"prompt_for_download"`
		}
		Profile struct {
			ExitType string `json:"exit_type"`
			Name     string
		}
	}
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	if p.Keep != 1 || p.Download.DefaultDirectory != `C:\dl` || p.Download.PromptForDownload || p.Profile.ExitType != "Normal" || p.Profile.Name != "p" {
		t.Fatalf("got %s", b)
	}
	if err := prepareProfile(t.TempDir(), `C:\dl`); err != nil { // fresh profile
		t.Fatal(err)
	}
}
