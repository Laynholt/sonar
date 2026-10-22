package doubledouble

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// ErrCheckClosed means the user closed the check window before passing the check.
var ErrCheckClosed = errors.New("the check window was closed before the check was passed")

// pollEvery is how often waitToken reads the profile; a variable so tests can speed it up.
var pollEvery = time.Second

// FindBrowser returns the path of Chrome, else Edge, or "" when neither is installed.
func FindBrowser() string {
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		pf, pf86, local := os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")
		const chrome, edge = `\Google\Chrome\Application\chrome.exe`, `\Microsoft\Edge\Application\msedge.exe`
		for _, root := range []string{pf, pf86, local} {
			if root != "" {
				candidates = append(candidates, root+chrome)
			}
		}
		for _, root := range []string{pf, pf86} {
			if root != "" {
				candidates = append(candidates, root+edge)
			}
		}
	case "darwin":
		candidates = []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	default:
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge"} {
			if p, err := exec.LookPath(name); err == nil {
				return p
			}
		}
		return ""
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Check opens a normal, visible browser window on the site for trackURL, with no debugging
// port, so Cloudflare sees a plain browser. The user passes the check there; the app only reads
// the token the site saves in the profile on disk, then closes the window.
func (c *Client) Check(ctx context.Context, browser, profileDir, trackURL string) error {
	if !c.checking.CompareAndSwap(false, true) {
		return errors.New("a doubledouble check is already open")
	}
	defer c.checking.Store(false)

	downloads := filepath.Join(profileDir, "check-downloads")
	if err := prepareProfile(profileDir, downloads); err != nil {
		return err
	}
	defer os.RemoveAll(downloads) // runs after the browser is gone: a file the site sent there is not kept
	cmd := exec.Command(browser,
		"--user-data-dir="+profileDir, "--disable-extensions", "--disable-sync", "--no-first-run",
		"--no-default-browser-check", "--new-window", c.Base+"/?url="+url.QueryEscape(trackURL+"?musicTerritory=US"))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start browser: %w", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	defer stopBrowser(cmd.Process, exited)

	tok, err := c.waitToken(ctx, profileDir, exited)
	if err != nil {
		return err
	}
	if err := c.SetToken(tok); err != nil {
		return fmt.Errorf("doubledouble: saving the check token: %w", err)
	}
	return nil
}

// prepareProfile drops the saved tabs and the old page storage (so neither a killed window
// nor an old token comes back), marks the last exit clean so no "restore pages" offer shows,
// and points browser downloads at downloadDir. Cookies stay, so Cloudflare may remember the browser.
func prepareProfile(profileDir, downloadDir string) error {
	def := filepath.Join(profileDir, "Default")
	for _, dir := range []string{"Sessions", "Session Storage"} {
		if err := os.RemoveAll(filepath.Join(def, dir)); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(def, 0o755); err != nil {
		return err
	}
	path := filepath.Join(def, "Preferences")
	prefs := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &prefs) // a broken file is replaced
	}
	section := func(name string) map[string]any {
		m, ok := prefs[name].(map[string]any)
		if !ok {
			m = map[string]any{}
			prefs[name] = m
		}
		return m
	}
	dl := section("download")
	dl["default_directory"], dl["prompt_for_download"], dl["directory_upgrade"] = downloadDir, false, true
	profile := section("profile")
	profile["exit_type"], profile["exited_cleanly"] = "Normal", true
	b, err := json.Marshal(prefs)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// waitToken reads the token from the profile's page storage once per pollEvery until the site saves it.
func (c *Client) waitToken(ctx context.Context, profileDir string, exited <-chan struct{}) (string, error) {
	origin := strings.TrimSuffix(c.Base, "/") + "/"
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		if tok := readToken(profileDir, origin); tok != "" {
			return tok, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-exited:
			if tok := readToken(profileDir, origin); tok != "" { // saved on the way out
				return tok, nil
			}
			return "", ErrCheckClosed
		case <-t.C:
		}
	}
}

// readToken returns sessionStorage.captchaToken of origin from the profile on disk, or "".
// Chromium keeps sessionStorage in a leveldb whose LOCK the running browser holds, so it
// reads a copy without the LOCK. Keys: "namespace-<tab>-<origin>" -> map id, "map-<id>-<key>" -> UTF-16LE value.
func readToken(profileDir, origin string) string {
	src := filepath.Join(profileDir, "Default", "Session Storage")
	entries, err := os.ReadDir(src)
	if err != nil {
		return ""
	}
	tmp, err := os.MkdirTemp("", "sonar-ss-")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmp)
	for _, e := range entries {
		if e.IsDir() || e.Name() == "LOCK" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil || os.WriteFile(filepath.Join(tmp, e.Name()), b, 0o600) != nil {
			return "" // mid-write; the next poll tries again
		}
	}
	db, err := leveldb.OpenFile(tmp, &opt.Options{ReadOnly: true, ErrorIfMissing: true, Strict: opt.NoStrict})
	if err != nil {
		return ""
	}
	defer db.Close()
	it := db.NewIterator(util.BytesPrefix([]byte("namespace-")), nil)
	defer it.Release()
	for it.Next() {
		if !strings.HasSuffix(string(it.Key()), "-"+origin) {
			continue
		}
		v, err := db.Get([]byte("map-"+string(it.Value())+"-captchaToken"), nil)
		if err != nil || len(v)%2 != 0 {
			continue
		}
		u := make([]uint16, len(v)/2)
		for i := range u {
			u[i] = uint16(v[2*i]) | uint16(v[2*i+1])<<8
		}
		if tok := string(utf16.Decode(u)); tok != "" {
			return tok
		}
	}
	return ""
}

// stopBrowser asks the browser to close like a user would, then kills it if it is still open after 3 s.
func stopBrowser(p *os.Process, exited <-chan struct{}) {
	select {
	case <-exited:
		return
	default:
	}
	askToClose(p)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		p.Kill()
	}
}
