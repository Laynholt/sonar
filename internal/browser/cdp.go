// Raw CDP plumbing: one websocket per target, navigate/evaluate calls only.
// Domain .enable calls stay out on purpose — the Cloudflare challenge detects
// them and never passes a browser they are used on.
package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// conn is a websocket speaking the CDP JSON protocol.
type conn struct {
	mu      sync.Mutex // serializes send and the pending map
	ws      *websocket.Conn
	next    int
	pending map[int]chan map[string]any
}

func dialRaw(url string) (*conn, error) {
	d := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	ws, _, err := d.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	c := &conn{ws: ws, pending: map[int]chan map[string]any{}}
	go c.read()
	return c, nil
}

func (c *conn) read() {
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			c.mu.Lock()
			for _, ch := range c.pending {
				ch <- nil // wake the callers up; they see the closed connection below
			}
			c.pending = map[int]chan map[string]any{}
			c.mu.Unlock()
			return
		}
		var m map[string]any
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		id, _ := m["id"].(float64)
		c.mu.Lock()
		ch, ok := c.pending[int(id)]
		delete(c.pending, int(id))
		c.mu.Unlock()
		if ok {
			ch <- m
		}
	}
}

// call sends a CDP command and returns its result; a nil result means the
// connection died (or an error came back, checked by the caller).
func (c *conn) call(method string, params map[string]any) (map[string]any, error) {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan map[string]any, 1)
	c.pending[id] = ch
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		c.mu.Unlock()
		return nil, err
	}
	err = c.ws.WriteMessage(websocket.TextMessage, req)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	var r map[string]any
	select {
	case r = <-ch:
	case <-time.After(30 * time.Second):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, errors.New("the CDP call timed out")
	}
	if r == nil {
		return nil, errors.New("the CDP connection closed")
	}
	if e, ok := r["error"].(map[string]any); ok {
		msg, _ := e["message"].(string)
		return nil, fmt.Errorf("%s: %s", method, msg)
	}
	res, _ := r["result"].(map[string]any)
	return res, nil
}

func (c *conn) close() {
	_ = c.ws.Close()
}

// devtools is the http/json endpoint of a running browser.
type devtools struct{ port string }

// target is one entry of /json/list.
type target struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	URL  string `json:"url"`
	WS   string `json:"webSocketDebuggerUrl"`
}

func (d devtools) get(path string, out any) error {
	c := &http.Client{Timeout: 5 * time.Second}
	r, err := c.Get("http://127.0.0.1:" + d.port + path)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func (d devtools) list() ([]target, error) {
	var ts []target
	return ts, d.get("/json/list", &ts)
}

// browserWS returns the browser-level websocket URL from /json/version.
func (d devtools) browserWS() (string, error) {
	var v struct {
		WS string `json:"webSocketDebuggerUrl"`
	}
	if err := d.get("/json/version", &v); err != nil {
		return "", err
	}
	return v.WS, nil
}

// connect waits for the DevTools endpoint and opens the browser-level websocket.
func (b *Browser) connect() error {
	d := devtools{b.port}
	var ts []target
	for i := 0; i < 30; i++ {
		time.Sleep(500 * time.Millisecond)
		var err error
		if ts, err = d.list(); err == nil {
			break
		}
	}
	if ts == nil {
		return fmt.Errorf("%w: the DevTools endpoint never came up", ErrBrowserGone)
	}
	wsURL, err := d.browserWS()
	if err != nil || wsURL == "" {
		return fmt.Errorf("%w: no browser target: %v", ErrBrowserGone, err)
	}
	bws, err := dialRaw(wsURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBrowserGone, err)
	}
	b.bws = bws
	return nil
}

// sitePage returns the site tab, creating and navigating it when needed.
func (b *Browser) sitePage(ctx context.Context, origin string) (*Page, error) {
	if b.page != nil {
		return b.page, nil
	}
	d := devtools{b.port}
	ts, err := d.list()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBrowserGone, err)
	}
	var t *target
	for i := range ts {
		if ts[i].Type == "page" {
			t = &ts[i]
			break
		}
	}
	if t == nil {
		return nil, fmt.Errorf("%w: no page target", ErrBrowserGone)
	}
	ws, err := dialRaw(t.WS)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBrowserGone, err)
	}
	b.page = &Page{b: b, id: t.ID, ws: ws}
	if err := b.page.Navigate(ctx, origin); err != nil {
		b.page = nil
		return nil, err
	}
	return b.page, nil
}

// hideWindow parks the window off-screen; it must happen after the challenge,
// which never passes for a window born (or parked) off-screen.
func (b *Browser) hideWindow(p *Page) {
	res, err := b.bws.call("Browser.getWindowForTarget", map[string]any{"targetId": p.id})
	if err != nil {
		return
	}
	id, _ := res["windowId"].(float64)
	_, _ = b.bws.call("Browser.setWindowBounds", map[string]any{
		"windowId": id,
		"bounds":   map[string]any{"left": -21333, "top": -21333},
	})
}

func (b *Browser) alive() bool {
	if b == nil || b.bws == nil || b.cmd == nil {
		return false
	}
	_, err := b.bws.call("Browser.getVersion", nil)
	return err == nil
}

// close shuts the browser down like a user would, then kills it as a backstop.
func (b *Browser) close() {
	if b == nil {
		return
	}
	if b.bws != nil {
		_, _ = b.bws.call("Browser.close", nil)
	}
	done := make(chan struct{})
	go func() { _, _ = b.cmd.Process.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = b.cmd.Process.Kill()
		<-done
	}
	b.bws, b.page, b.dlID = nil, nil, ""
}

// Download fetches url in a spare tab and saves the file into dir, reporting
// byte progress. Navigation, not fetch: the download redirects to a file host
// whose CORS never lets an in-page fetch follow. Chrome does not deliver CDP
// download events anymore, so completion is detected by polling: the file is
// done when its size is stable and non-zero across a settle interval.
func (b *Browser) download(ctx context.Context, url, dir string, onProgress func(done, total int64)) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	behavior := map[string]any{"behavior": "allowAndName", "downloadPath": dir}
	if _, err := b.bws.call("Browser.setDownloadBehavior", behavior); err != nil {
		return "", err
	}
	defer func() { _, _ = b.bws.call("Browser.setDownloadBehavior", map[string]any{"behavior": "default"}) }()

	dl, err := b.spareTab(ctx)
	if err != nil {
		return "", err
	}
	if _, err := dl.ws.call("Page.navigate", map[string]any{"url": url}); err != nil {
		return "", err
	}
	debugf("download: navigating %s", url)
	go func() { // a cancel must not leave the download running
		<-ctx.Done()
		if b.bws != nil {
			_, _ = b.bws.call("Target.closeTarget", map[string]any{"targetId": dl.id})
			b.dlMu.Lock()
			if b.dlID == dl.id {
				b.dlID = ""
			}
			b.dlMu.Unlock()
		}
	}()

	const settle = 2 * time.Second     // size stability across this means the file is complete
	const noFileWait = 4 * time.Minute // the file host may answer its own challenge first
	start := time.Now()
	lastTitleCheck := time.Now()
	hotlinkRetries := 0
	for {
		// the file hosts guard downloads with hotlink protection: the first
		// navigation gets a 404 page whose hidden iframe sets the clearance,
		// and the same URL works once it is navigated again
		if time.Since(lastTitleCheck) > 5*time.Second {
			lastTitleCheck = time.Now()
			if title, terr := dl.Title(context.Background()); terr == nil {
				debugf("download: dl tab title %q", title)
				t := strings.ToLower(title)
				if strings.Contains(t, "404") || strings.Contains(t, "not found") {
					if hotlinkRetries >= 2 {
						return "", errors.New("the file host kept answering 404")
					}
					hotlinkRetries++
					debugf("download: hotlink retry %d", hotlinkRetries)
					if _, err := dl.ws.call("Page.navigate", map[string]any{"url": url}); err != nil {
						return "", err
					}
				}
			}
		}
		if newest := latestFile(dir); newest != "" {
			s1, err := os.Stat(newest)
			if err == nil && s1.Size() > 0 {
				time.Sleep(settle)
				if s2, err := os.Stat(newest); err == nil && s2.Size() == s1.Size() {
					if onProgress != nil {
						onProgress(s2.Size(), s2.Size())
					}
					return newest, nil
				}
				if onProgress != nil {
					onProgress(s1.Size(), 0)
				}
			}
		}
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if time.Since(start) > noFileWait {
			if title, terr := dl.Title(context.Background()); terr == nil {
				debugf("download: stuck, dl tab title %q", title)
			}
			return "", errors.New("download did not start")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// debugf logs to stderr when SONAR_BROWSER_DEBUG is set, for live diagnostics.
func debugf(format string, args ...any) {
	if os.Getenv("SONAR_BROWSER_DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "browser: "+format+"\n", args...)
	}
}

// spares a second tab for downloads, so the site tab keeps its origin.
func (b *Browser) spareTab(ctx context.Context) (*Page, error) {
	b.dlMu.Lock()
	defer b.dlMu.Unlock()
	if b.dlID == "" {
		res, err := b.bws.call("Target.createTarget", map[string]any{"url": "about:blank"})
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrBrowserGone, err)
		}
		id, _ := res["targetId"].(string)
		b.dlID = id
	}
	d := devtools{b.port}
	ts, err := d.list()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBrowserGone, err)
	}
	for _, t := range ts {
		if t.ID == b.dlID && t.WS != "" {
			ws, err := dialRaw(t.WS)
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrBrowserGone, err)
			}
			return &Page{b: b, id: t.ID, ws: ws}, nil
		}
	}
	b.dlID = "" // the tab is gone; the next download gets a fresh one
	return nil, fmt.Errorf("%w: the download tab is gone", ErrBrowserGone)
}

// latestFile returns the regular file in dir with the most recent modification, or "".
func latestFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var best string
	var bestMod time.Time
	for _, e := range entries {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".crdownload") {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() == 0 {
			continue // an empty file may still be filling; the next poll sees it
		}
		if best == "" || info.ModTime().After(bestMod) {
			best, bestMod = filepath.Join(dir, e.Name()), info.ModTime()
		}
	}
	return best
}

// Title returns the tab title, "" while it has none.
func (p *Page) Title(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	v, err := p.eval(`document.title`)
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
}

// Navigate loads url in the tab.
func (p *Page) Navigate(ctx context.Context, url string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, err := p.ws.call("Page.navigate", map[string]any{"url": url})
	return err
}

// eval runs a JS expression and returns its value. It uses Runtime.evaluate's
// awaitPromise so async snippets just work; returnByValue keeps plain JSON types.
func (p *Page) eval(expr string) (any, error) {
	// userGesture stays off: it fakes user activation, which the challenge
	// can read off navigator.userActivation and never passes.
	res, err := p.ws.call("Runtime.evaluate", map[string]any{
		"expression":    expr,
		"awaitPromise":  true,
		"returnByValue": true,
	})
	if err != nil {
		return nil, err
	}
	if ed, ok := res["exceptionDetails"].(map[string]any); ok {
		text, _ := ed["text"].(string)
		if exc, ok := ed["exception"].(map[string]any); ok {
			if d, _ := exc["description"].(string); d != "" {
				text = d
			} else if dv, _ := exc["value"].(string); dv != "" {
				text = dv
			}
		}
		return nil, fmt.Errorf("eval: %s", text)
	}
	r, _ := res["result"].(map[string]any)
	return r["value"], nil
}

// Fetch performs an HTTP request from the tab's origin context. It returns the
// HTTP status of the final response without rejecting non-2xx ones.
func (p *Page) Fetch(ctx context.Context, method, url string, body []byte, headers map[string]string) (int, []byte, error) {
	if headers == nil {
		headers = map[string]string{}
	}
	opts := map[string]any{"url": url, "method": method, "headers": headers}
	if body != nil {
		opts["body"] = base64.StdEncoding.EncodeToString(body)
		opts["hasBody"] = true
	}
	cfg, err := json.Marshal(opts)
	if err != nil {
		return 0, nil, err
	}
	js := fmt.Sprintf(`(async () => {
		const o = %s;
		const init = { method: o.method, headers: o.headers };
		if (o.hasBody) init.body = Uint8Array.from(atob(o.body), (ch) => ch.charCodeAt(0));
		const r = await fetch(o.url, init);
		const buf = new Uint8Array(await r.arrayBuffer());
		let bin = '';
		for (let i = 0; i < buf.length; i += 0x8000) bin += String.fromCharCode.apply(null, buf.subarray(i, i + 0x8000));
		return JSON.stringify({ s: r.status, b: btoa(bin) });
	})()`, cfg)
	p.mu.Lock()
	v, err := p.eval(js)
	p.mu.Unlock()
	if err != nil {
		return 0, nil, err
	}
	text, ok := v.(string)
	if !ok {
		return 0, nil, fmt.Errorf("the page returned %T instead of a result", v)
	}
	var out struct {
		S int    `json:"s"`
		B string `json:"b"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return 0, nil, err
	}
	b, err := base64.StdEncoding.DecodeString(out.B)
	if err != nil {
		return 0, nil, err
	}
	return out.S, b, nil
}

// ProbeDownload asks for the first byte of a download URL and reports the final
// URL (after redirects) and content type.
func (p *Page) ProbeDownload(ctx context.Context, url string) (finalURL, contentType string, err error) {
	js := fmt.Sprintf(`(async () => {
		const r = await fetch(%s, { headers: { range: 'bytes=0-0' } });
		r.body.cancel();
		return JSON.stringify({ u: r.url, t: r.headers.get('content-type') });
	})()`, jsonString(url))
	p.mu.Lock()
	v, err := p.eval(js)
	p.mu.Unlock()
	if err != nil {
		return "", "", err
	}
	text, ok := v.(string)
	if !ok {
		return "", "", fmt.Errorf("the page returned %T instead of a result", v)
	}
	var out struct {
		U string `json:"u"`
		T string `json:"t"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return "", "", err
	}
	return out.U, out.T, nil
}

// SolveTurnstile renders a Cloudflare Turnstile widget on the tab and waits for
// its token. The widget solves itself when the browser passes the invisible
// checks; a widget requires a rendered container, hence the fixed overlay.
func (p *Page) SolveTurnstile(ctx context.Context, sitekey string, maxWait time.Duration) (string, error) {
	js := fmt.Sprintf(`(async () => {
		for (let i = 0; i < 40 && !document.body; i++) await new Promise((r) => setTimeout(r, 250));
		if (!document.body) return 'ERR no body: ' + document.readyState + ' ' + location.href.slice(0, 80) + ' TITLE=' + document.title;
		const w = document.createElement('div');
		w.style.cssText = 'position:fixed;left:0;bottom:0;z-index:2147483647;';
		document.body.appendChild(w);
		const token = await new Promise((resolve) => {
			let done = false;
			const end = (v) => { if (!done) { done = true; resolve(v); } };
			let widget = null;
			const render = () => {
				try {
					widget = turnstile.render(w, { sitekey: %s, callback: (t) => end(t), 'error-callback': (e) => {
						const code = String(e || '');
						if (widget != null && (code.startsWith('300') || code.startsWith('600'))) {
							// the site's own page re-renders on these codes
							turnstile.remove(widget);
							widget = null;
							setTimeout(render, 1000);
							return;
						}
						end('ERR ' + code);
					} });
				} catch (e) { end('ERR ' + e); }
			};
			render();
			setTimeout(() => end('TIMEOUT'), %d);
		});
		w.remove();
		return token;
	})()`, jsonString(sitekey), maxWait.Milliseconds())
	p.mu.Lock()
	v, err := p.eval(js)
	p.mu.Unlock()
	if err != nil {
		return "", err
	}
	t, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("the page returned %T instead of a result", v)
	}
	switch {
	case t == "TIMEOUT":
		return "", ErrChallengeStuck
	case strings.HasPrefix(t, "ERR "):
		return "", fmt.Errorf("%w: %s", ErrTurnstileFailed, strings.TrimPrefix(t, "ERR "))
	case t == "":
		return "", errors.New("turnstile rendered nothing")
	default:
		return t, nil
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
