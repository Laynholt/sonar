// Package browser drives the user's real Chrome or Edge over a minimal raw
// CDP connection, so download sites behind Cloudflare see a genuine browser.
// Plain HTTP clients are refused even with a stolen clearance cookie, and
// fuller automation stacks get caught by the challenge, which loops forever
// once DevTools domains like Runtime.enable are switched on: a bare websocket
// that only navigates and evaluates passes invisibly. The client launches
// lazily, keeps one persistent profile per plugin (its clearance cookie
// survives restarts), and can relaunch as a plain visible window for a manual
// check when the hidden pass is refused.
package browser

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ErrChallengeStuck means the Cloudflare challenge did not clear by itself within its budget.
var ErrChallengeStuck = errors.New("the Cloudflare challenge did not pass by itself")

// ErrTurnstileFailed means the Turnstile widget refused to solve after a
// re-render (its retryable error codes): the visible check is the way forward.
var ErrTurnstileFailed = errors.New("the CAPTCHA did not solve itself")

// ErrBrowserGone means the browser process is no longer reachable (it was closed or crashed).
var ErrBrowserGone = errors.New("the browser is gone")

// challengeWait is how long Page waits for a challenge to clear by itself before
// giving up and letting the caller offer the visible check. A variable so tests can shorten it.
var challengeWait = 60 * time.Second

// VisibleWait bounds the manual check in a visible window; sonar lets an action
// run for 10 minutes, so the check gets a bit less than that.
const VisibleWait = 9 * time.Minute

// Page is a browser tab the client talks to.
type Page struct {
	b  *Browser
	mu sync.Mutex // one CDP call at a time on this tab
	id string     // the DevTools target id
	ws *conn
}

// Browser is one launched browser process plus its connections.
type Browser struct {
	headless bool   // the mode this launch used
	port     string // the DevTools port of the running browser
	cmd      *exec.Cmd
	bws      *conn  // the browser-level websocket, for tab management and downloads
	page     *Page  // the site tab
	dlID     string // the spare tab downloads run in
	dlMu     sync.Mutex
}

// Client manages one lazily launched browser for a plugin.
type Client struct {
	mu       sync.Mutex // serializes launch, close and mode switches
	dataDir  string     // the plugin data dir; the profile lives in <dataDir>/browser
	proxy    string     // the --proxy-server value; "" means direct
	headless bool       // the mode the next launch uses; starts true
	browser  *Browser
}

// New returns a client storing its profile and downloads under the plugin data dir.
func New(dataDir string) *Client {
	return &Client{dataDir: dataDir, headless: true}
}

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

// SetProxy points the browser at proxy as sonar's effectiveProxy does ("" for direct).
// A running browser is dropped, so the next use relaunches with the new value.
func (c *Client) SetProxy(proxy string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if proxy == c.proxy {
		return
	}
	c.proxy = proxy
	if c.browser != nil {
		c.browser.close()
		c.browser = nil
	}
}

// SetVisible relaunches the browser with a window (v) or hidden (!v). It never
// launches anything by itself: the next Page call does, in the requested mode.
func (c *Client) SetVisible(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.browser == nil {
		c.headless = !v
		return
	}
	if c.browser.headless == !v {
		return
	}
	c.browser.close()
	c.browser = nil
	c.headless = !v
}

// Close shuts the browser down. The plugin's job-object kill is the backstop.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.browser != nil {
		c.browser.close()
		c.browser = nil
	}
}

// Alive reports whether the launched browser is still reachable.
func (c *Client) Alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.browser != nil && c.browser.alive()
}

// DownloadsDir is where Download puts its per-job subdirectories; the client
// clears stale ones on launch, because a job-object kill leaves half-files behind.
func (c *Client) DownloadsDir() string {
	return filepath.Join(c.dataDir, "browser-downloads")
}

// launchLocked starts the browser and clears stale downloads. Headless mode is
// never used: it still identifies itself as HeadlessChrome and the challenge
// never passes it. The hidden mode is a real window parked off-screen.
func (c *Client) launchLocked() (*Browser, error) {
	if c.browser != nil && c.browser.alive() {
		return c.browser, nil
	}
	if c.browser != nil {
		c.browser.close()
		c.browser = nil
	}
	bin := FindBrowser()
	if bin == "" {
		return nil, errors.New("no Chromium-based browser (Chrome or Edge) was found")
	}
	profile := filepath.Join(c.dataDir, "browser")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(c.DownloadsDir()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args := []string{
		"--remote-debugging-port=" + port,
		"--user-data-dir=" + profile,
		"--no-first-run", "--no-default-browser-check", "--disable-search-engine-choice-screen",
		"--password-store=basic", "--use-mock-keychain", "--disable-background-networking",
		"--disable-background-timer-throttling", "--disable-backgrounding-occluded-windows",
		"--disable-breakpad", "--disable-client-side-phishing-detection", "--disable-component-update",
		"--disable-default-apps", "--disable-dev-shm-usage", "--disable-extensions",
		"--disable-features=Translate,TranslateUI,OptimizationHints", "--disable-hang-monitor",
		"--disable-ipc-flooding-protection", "--disable-popup-blocking", "--disable-prompt-on-repost",
		"--disable-sync", "--force-color-profile=srgb", "--metrics-recording-only",
		"--safebrowsing-disable-auto-update", "--disable-blink-features=AutomationControlled",
	}
	args = append(args, "--window-size=1280,800") // the window is born on-screen; hideWindow parks it after the challenge
	// no --proxy-server when the user has none: forcing direct makes the
	// challenge loop forever, while the system default path passes it
	if c.proxy != "" {
		args = append(args, "--proxy-server="+c.proxy)
	}
	args = append(args, "about:blank")
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = launchSysProcAttr()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("launch browser: %w", err)
	}
	b := &Browser{headless: c.headless, port: port, cmd: cmd}
	if err := b.connect(); err != nil {
		b.close()
		return nil, err
	}
	// let the browser settle before its first navigation: a page requested the
	// moment the process came up is an automation tell
	time.Sleep(10 * time.Second)
	c.browser = b
	return b, nil
}

// Page returns the site tab for origin, launching the browser if needed, and waits
// for a Cloudflare challenge on it to clear by itself (challengeWait budget).
// In the hidden mode the window starts on-screen — the challenge reads the window
// position and never passes a window born off-screen — and moves off-screen once
// the challenge is through.
func (c *Client) Page(ctx context.Context, origin string) (*Page, error) {
	p, err := c.PageFor(ctx, origin)
	if err != nil {
		return nil, err
	}
	if err := c.WaitChallenge(ctx, p, challengeWait); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.browser != nil && c.headless {
		c.browser.hideWindow(p)
	}
	c.mu.Unlock()
	return p, nil
}

// PageFor returns the site tab without waiting for a challenge. The tab lives
// between downloads: its cookies and clearance make later calls cheap.
func (c *Client) PageFor(ctx context.Context, origin string) (*Page, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, err := c.launchLocked()
	if err != nil {
		return nil, err
	}
	return b.sitePage(ctx, strings.TrimSuffix(origin, "/")+"/")
}

// WaitChallenge polls the tab title until the challenge clears (a challenge page
// is titled "Just a moment..." and the like) or the budget runs out.
func (c *Client) WaitChallenge(ctx context.Context, p *Page, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		title, err := p.Title(ctx)
		if err != nil {
			if !c.Alive() {
				return ErrBrowserGone
			}
			title = "Just a moment..." // mid-navigation; keep polling
		}
		if !isChallengeTitle(title) {
			return nil
		}
		if time.Now().After(deadline) {
			return ErrChallengeStuck
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func isChallengeTitle(title string) bool {
	// an empty title means the page has not loaded yet
	t := strings.ToLower(title)
	return t == "" || strings.Contains(t, "moment") || strings.Contains(t, "момент") ||
		strings.Contains(t, "attention") || strings.Contains(t, "внимание") ||
		strings.Contains(t, "blocked")
}

// Fetch performs an HTTP request from the tab's origin context, so the site sees
// its own browser: cookies and Cloudflare clearance travel with it. It returns the
// HTTP status of the final response without rejecting non-2xx ones; callers keep
// the site's own error semantics.
func (c *Client) Fetch(ctx context.Context, p *Page, method, url string, body []byte, headers map[string]string) (status int, respBody []byte, err error) {
	return p.Fetch(ctx, method, url, body, headers)
}

// ProbeDownload asks for the first byte of a download URL and reports the final
// URL (after redirects) and content type, so the caller can pick the extension
// before committing to the whole file.
func (c *Client) ProbeDownload(ctx context.Context, p *Page, url string) (finalURL, contentType string, err error) {
	return p.ProbeDownload(ctx, url)
}

// SolveTurnstile renders a Cloudflare Turnstile widget on the tab and waits for
// its token. The widget solves itself when the browser passes the invisible
// checks; a widget requires a rendered container, hence the fixed overlay.
func (c *Client) SolveTurnstile(ctx context.Context, p *Page, sitekey string, maxWait time.Duration) (string, error) {
	return p.SolveTurnstile(ctx, sitekey, maxWait)
}

// Download fetches url in a spare tab and saves the file into dir, reporting
// byte progress. It returns the downloaded file's path. The lock is held only
// for the launch: a long download must not block $/settings or $/cancel, which
// reach the plugin through the same reader goroutine.
func (c *Client) Download(ctx context.Context, url, dir string, onProgress func(done, total int64)) (string, error) {
	c.mu.Lock()
	b, err := c.launchLocked()
	c.mu.Unlock()
	if err != nil {
		return "", err
	}
	return b.download(ctx, url, dir, onProgress)
}

func freePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer l.Close()
	return l.Addr().String()[strings.LastIndex(l.Addr().String(), ":")+1:], nil
}
