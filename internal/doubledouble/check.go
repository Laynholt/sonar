package doubledouble

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ErrCheckClosed means the user closed the check window before passing the check.
var ErrCheckClosed = errors.New("the check window was closed before the check was passed")

// pollEvery is how often watch reads the page; a variable so tests can speed it up.
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

// Check opens a normal, visible browser window on the site for trackURL. The user passes the
// check there; the app only reads the token the site stores, denies downloads and closes the window.
func (c *Client) Check(ctx context.Context, browser, profileDir, trackURL string) error {
	if !c.checking.CompareAndSwap(false, true) {
		return errors.New("a doubledouble check is already open")
	}
	defer c.checking.Store(false)

	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(profileDir, "DevToolsActivePort")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := clearSession(profileDir); err != nil {
		return err
	}
	cmd := exec.Command(browser,
		"--user-data-dir="+profileDir, "--remote-debugging-port=0", "--no-first-run", "--no-default-browser-check",
		"--new-window", c.Base+"/?url="+url.QueryEscape(trackURL+"?musicTerritory=US"))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start browser: %w", err)
	}
	exited := make(chan struct{})
	go func() { cmd.Wait(); close(exited) }()
	defer func() {
		select {
		case <-exited:
		case <-time.After(3 * time.Second):
			cmd.Process.Kill()
		}
	}()

	pctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-exited:
			cancel()
		case <-pctx.Done():
		}
	}()
	port, browserPath, err := readDevToolsPort(pctx, profileDir)
	if err != nil {
		select {
		case <-exited:
			return ErrCheckClosed
		default:
			return err
		}
	}
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	_, err = c.watch(ctx, endpoint, browserPath, exited)
	return err
}

// clearSession drops the saved tabs, so a window that was killed last time is not restored
// next to the new one; cookies stay, so Cloudflare may remember the browser.
func clearSession(profileDir string) error {
	return os.RemoveAll(filepath.Join(profileDir, "Default", "Sessions"))
}

// readDevToolsPort waits for the browser to write DevToolsActivePort: line 1 is the port, line 2 the browser socket path.
func readDevToolsPort(ctx context.Context, profileDir string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		if b, err := os.ReadFile(filepath.Join(profileDir, "DevToolsActivePort")); err == nil {
			if lines := strings.Fields(string(b)); len(lines) >= 2 {
				if port, err := strconv.Atoi(lines[0]); err == nil {
					return port, lines[1], nil
				}
			}
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return 0, "", errors.New("the browser did not report its debugging port")
			}
			return 0, "", ctx.Err()
		case <-t.C:
		}
	}
}

// watch reads sessionStorage.captchaToken from the check page once per pollEvery until it is set.
// It denies downloads on the browser socket, then stores the token and closes the browser over
// that same socket, so the denial holds until the window is gone.
func (c *Client) watch(ctx context.Context, endpoint, browserPath string, exited <-chan struct{}) (string, error) {
	var browser, page *cdpConn
	defer func() {
		for _, x := range []*cdpConn{browser, page} {
			if x != nil {
				x.ws.Close()
			}
		}
	}()
	hc := &http.Client{Timeout: 3 * time.Second} // loopback DevTools endpoint: no site proxy or UA needed
	pageSeen := false
	step := func() (string, error) {
		if browser == nil {
			b, err := dialCDP(ctx, wsURL(endpoint)+browserPath)
			if err != nil {
				return "", nil // not listening yet
			}
			browser = b
			if _, err := b.call("Browser.setDownloadBehavior", map[string]string{"behavior": "deny"}); err != nil {
				return "", fmt.Errorf("deny downloads: %w", err)
			}
		}
		if page == nil {
			wsPage, err := findPage(ctx, hc, endpoint, c.Base)
			if err != nil || wsPage == "" {
				if pageSeen {
					return "", ErrCheckClosed // the browser is gone, or the tab was closed
				}
				return "", nil
			}
			pageSeen = true
			if page, err = dialCDP(ctx, wsPage); err != nil {
				return "", nil
			}
		}
		res, err := page.call("Runtime.evaluate", map[string]any{"expression": "sessionStorage.getItem('captchaToken')", "returnByValue": true})
		if err != nil {
			page.ws.Close()
			page = nil // re-find the page next tick
			return "", nil
		}
		var r struct {
			Result struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"result"`
		}
		if json.Unmarshal(res, &r) == nil && r.Result.Type == "string" {
			return r.Result.Value, nil
		}
		return "", nil
	}

	t := time.NewTicker(pollEvery)
	defer t.Stop()
	for {
		tok, err := step()
		if tok != "" {
			saveErr := c.SetToken(tok)
			browser.call("Browser.close", nil) // best effort: Check's deferred kill covers failure
			if saveErr != nil {
				return "", fmt.Errorf("doubledouble: saving the check token: %w", saveErr)
			}
			return tok, nil
		}
		if err != nil {
			select {
			case <-exited:
				return "", ErrCheckClosed
			default:
				return "", err
			}
		}
		select {
		case <-ctx.Done():
			if browser != nil {
				browser.call("Browser.close", nil) // close it cleanly, so it does not offer to restore the tab
			}
			return "", ctx.Err()
		case <-exited:
			return "", ErrCheckClosed
		case <-t.C:
		}
	}
}

// findPage returns the debugger socket of the first page open on the site, or "".
func findPage(ctx context.Context, hc *http.Client, endpoint, base string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/json/list", http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var targets []struct{ Type, URL, WebSocketDebuggerURL string }
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return "", err
	}
	for _, tg := range targets {
		if tg.Type == "page" && strings.HasPrefix(tg.URL, base) {
			return tg.WebSocketDebuggerURL, nil
		}
	}
	return "", nil
}

func wsURL(endpoint string) string { return "ws" + strings.TrimPrefix(endpoint, "http") }

type cdpConn struct {
	ws *websocket.Conn
	n  int
}

func dialCDP(ctx context.Context, u string) (*cdpConn, error) {
	ws, _, err := websocket.DefaultDialer.DialContext(ctx, u, nil)
	if err != nil {
		return nil, err
	}
	return &cdpConn{ws: ws}, nil
}

// call sends one DevTools command and returns its result, skipping events until the reply with the same id.
func (c *cdpConn) call(method string, params any) (json.RawMessage, error) {
	c.n++
	c.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	c.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if params == nil {
		params = struct{}{} // Chromium may reject "params":null
	}
	if err := c.ws.WriteJSON(map[string]any{"id": c.n, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		var m struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := c.ws.ReadJSON(&m); err != nil {
			return nil, err
		}
		if m.ID != c.n {
			continue
		}
		if m.Error != nil {
			return nil, errors.New(m.Error.Message)
		}
		return m.Result, nil
	}
}
