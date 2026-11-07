// Package lucida is the lucida.to download site. The site sits behind a full
// Cloudflare challenge and its file hosts answer plain HTTP clients with
// deceptive 404s even when given a clearance cookie, so live requests all go
// through the browser (internal/browser); plain HTTP remains for tests and a
// fake site.
package lucida

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sonar/internal/browser"
	"sonar/internal/download"
	"sonar/internal/httpx"
	"sonar/internal/source"
)

type Client struct {
	Base      string
	PollEvery time.Duration
	HTTP      *http.Client // plain mode only; browser mode ignores it

	bc *browser.Client // set: every request goes through the browser
}

// New returns a plain-HTTP client: it only works against a fake site or when
// Cloudflare is off, which is what the tests need.
func New() *Client {
	return &Client{
		HTTP:      &http.Client{Transport: httpx.Client().Transport},
		Base:      "https://lucida.to",
		PollEvery: time.Second,
	}
}

// NewBrowser returns a client reaching lucida.to through bc: the challenge pass,
// the API calls and the file download all happen in the browser.
func NewBrowser(bc *browser.Client) *Client {
	return &Client{
		Base:      "https://lucida.to",
		PollEvery: time.Second,
		bc:        bc,
	}
}

func (c *Client) Name() string { return "lucida" }

// errRetryDownload marks a download attempt worth repeating with a fresh job:
// the site hands out several file servers and some answer a bare 404.
var errRetryDownload = errors.New("retry the download with a new job")

// Check runs the visible fallback for the Cloudflare challenge: the browser
// relaunches with a window, the user completes the check there, and later
// downloads run headless again. It is a no-op without the browser backend.
func (c *Client) Check(ctx context.Context) error {
	if c.bc == nil {
		return nil
	}
	c.bc.SetVisible(true)
	defer c.bc.SetVisible(false)
	page, err := c.bc.PageFor(ctx, c.Base)
	if err != nil {
		return mapBrowserErr(err)
	}
	if err := c.bc.WaitChallenge(ctx, page, browser.VisibleWait); err != nil {
		return mapBrowserErr(err)
	}
	return nil
}

func (c *Client) api(path string) string { return c.Base + "/api/load?url=" + url.QueryEscape(path) }

// mapBrowserErr turns browser failures into the contract's error kinds.
func mapBrowserErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, browser.ErrChallengeStuck):
		return fmt.Errorf("lucida: %w: the Cloudflare challenge did not pass, press the check button", source.ErrNeedsCheck)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return fmt.Errorf("lucida: %w: %v", source.ErrUnavailable, err)
	}
}

// request sends the request and returns the raw answer with its HTTP status.
// Plain mode talks HTTP directly; browser mode fetches from the site tab.
func (c *Client) request(ctx context.Context, method, u string, body []byte) (int, []byte, error) {
	if c.bc == nil {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if cerr := ctx.Err(); cerr != nil {
				return 0, nil, cerr
			}
			return 0, nil, fmt.Errorf("lucida: %w: %v", source.ErrUnavailable, err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return 0, nil, fmt.Errorf("lucida: %w: %v", source.ErrUnavailable, err)
		}
		return resp.StatusCode, b, nil
	}
	page, err := c.bc.Page(ctx, c.Base)
	if err != nil {
		return 0, nil, mapBrowserErr(err)
	}
	headers := map[string]string{}
	if body != nil {
		headers["content-type"] = "application/json"
	}
	status, b, err := c.bc.Fetch(ctx, page, method, u, body, headers)
	// Cloudflare blips: a request right after the clearance lands can be
	// challenged or dropped. Settle, re-check the tab and try twice more.
	for attempt := 0; err != nil && attempt < 2; attempt++ {
		select {
		case <-ctx.Done():
			return 0, nil, cerr(ctx)
		case <-time.After(10 * time.Second):
		}
		if werr := c.bc.WaitChallenge(ctx, page, 30*time.Second); werr != nil {
			return 0, nil, mapBrowserErr(werr)
		}
		status, b, err = c.bc.Fetch(ctx, page, method, u, body, headers)
	}
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return 0, nil, cerr
		}
		return 0, nil, fmt.Errorf("lucida: %w: %v", source.ErrUnavailable, err)
	}
	return status, b, nil
}

// cerr returns the context error inside ctx, or nil.
func cerr(ctx context.Context) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return nil
}

// statusError keeps source.CheckStatus's semantics for a bare status code.
func statusError(status int) error {
	return source.CheckStatus("lucida", &http.Response{StatusCode: status})
}

// fetchJSON decodes a JSON answer into out. Network errors, 403/429/5xx and
// non-JSON answers (a Cloudflare page) wrap source.ErrUnavailable.
func (c *Client) fetchJSON(ctx context.Context, method, u string, body, out any) error {
	var raw []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		raw = b
	}
	status, b, err := c.request(ctx, method, u, raw)
	if err != nil {
		return err
	}
	if err := statusError(status); err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("lucida: %w: answer is not JSON: %v", source.ErrUnavailable, err)
	}
	return nil
}

// message is the site's explanation of a failed answer.
func message(m map[string]any) string {
	for _, k := range []string{"error", "message"} {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return fmt.Sprint(m)
}

func (c *Client) Download(ctx context.Context, trackURL, pathNoExt string, onStatus func(string)) (string, error) {
	var path string
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			onStatus("retrying with a different file server...")
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
		path, err = c.downloadOnce(ctx, trackURL, pathNoExt, onStatus)
		if err == nil || !errors.Is(err, errRetryDownload) {
			return path, err
		}
	}
	return "", err
}

func (c *Client) downloadOnce(ctx context.Context, trackURL, pathNoExt string, onStatus func(string)) (string, error) {
	var data struct {
		Nodes []struct {
			Type string `json:"type"`
			Data []any  `json:"data"`
		} `json:"nodes"`
	}
	if err := c.fetchJSON(ctx, http.MethodGet, c.Base+"/__data.json?url="+url.QueryEscape(trackURL)+"&country=auto", nil, &data); err != nil {
		return "", err
	}
	var page map[string]any
	for _, n := range data.Nodes {
		if p, _ := unflatten(n.Data).(map[string]any); n.Type == "data" && p["info"] != nil {
			page = p
			break
		}
	}
	info, _ := page["info"].(map[string]any)
	if info == nil {
		return "", errors.New("lucida: unexpected __data.json format (site changed?)")
	}
	if ok, has := info["success"].(bool); has && !ok {
		return "", fmt.Errorf("lucida: %s", message(info))
	}
	if typ, _ := info["type"].(string); typ != "track" {
		return "", fmt.Errorf("lucida: expected a track, got %s", typ)
	}

	// The page token is base64 of base64 of the primary token.
	tok, _ := page["token"].(string)
	for range 2 {
		b, err := base64.StdEncoding.DecodeString(tok)
		if err != nil {
			return "", fmt.Errorf("lucida: bad page token: %w", err)
		}
		tok = string(b)
	}
	if onStatus == nil {
		onStatus = func(string) {}
	}

	var start map[string]any
	err := c.fetchJSON(ctx, http.MethodPost, c.api("/api/fetch/stream/v2"), map[string]any{
		"url": page["download"], "metadata": false, "compat": false, "private": true, "handoff": true, // sonar writes the tags itself
		"account":   map[string]any{"type": "country", "id": "auto"},
		"upload":    map[string]any{"enabled": false, "service": "pixeldrain"},
		"downscale": "original",
		"token":     map[string]any{"primary": tok, "expiry": page["tokenExpiry"]},
	}, &start)
	if err != nil {
		return "", err
	}
	handoff, _ := start["handoff"].(string)
	server, _ := start["name"].(string)
	if ok, _ := start["success"].(bool); !ok || handoff == "" {
		return "", fmt.Errorf("lucida: %s", message(start))
	}

	req := "/api/fetch/request/" + handoff
	force := "&force=" + url.QueryEscape(server)
	for {
		var st map[string]any
		if err := c.fetchJSON(ctx, http.MethodGet, c.api(req)+force, nil, &st); err != nil {
			return "", err
		}
		if ok, _ := st["success"].(bool); !ok || st["status"] == "error" {
			return "", fmt.Errorf("lucida: %s", message(st))
		}
		if st["status"] == "completed" {
			break
		}
		if m, _ := st["message"].(string); m != "" {
			onStatus(strings.ReplaceAll(m, "{item}", "track"))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(c.PollEvery):
		}
	}

	dlURL := c.api(req+"/download") + force + "&redirect=true"
	if c.bc == nil {
		return c.savePlain(ctx, dlURL, pathNoExt)
	}
	return c.saveViaBrowser(ctx, dlURL, pathNoExt)
}

// savePlain downloads with the plain HTTP client and keeps source.Save's behavior.
func (c *Client) savePlain(ctx context.Context, dlURL, pathNoExt string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dlURL, http.NoBody)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		return "", fmt.Errorf("lucida: %w: %v", source.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if err := statusError(resp.StatusCode); err != nil {
		return "", err
	}
	path, err := source.Save(resp, pathNoExt)
	if err != nil {
		return "", fmt.Errorf("lucida: %w", err)
	}
	return path, nil
}

// saveViaBrowser probes the download URL for its content type when it can, lets
// the browser fetch the file (the file hosts challenge a direct fetch's CORS and
// refuse plain HTTP), and moves it next to pathNoExt under the right extension.
func (c *Client) saveViaBrowser(ctx context.Context, dlURL, pathNoExt string) (string, error) {
	page, err := c.bc.PageFor(ctx, c.Base)
	if err != nil {
		return "", mapBrowserErr(err)
	}
	ext := ""
	if _, ct, err := c.bc.ProbeDownload(ctx, page, dlURL); err == nil {
		ext = extFromType(ct)
		if ext == "" && strings.HasPrefix(ct, "application/zip") {
			return "", fmt.Errorf("lucida: %w: got an archive instead of one track", source.ErrUnavailable)
		}
	}
	if err := os.MkdirAll(c.bc.DownloadsDir(), 0o755); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(c.bc.DownloadsDir(), "lucida-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path, err := c.bc.Download(ctx, dlURL, dir, func(done, total int64) {
		download.Report(ctx, done, total)
	})
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		return "", fmt.Errorf("%w: %v", errRetryDownload, err)
	}
	if ext == "" {
		ext = sniffExt(path)
	}
	if ext == "" {
		return "", fmt.Errorf("lucida: %w: the download is not audio", source.ErrUnavailable)
	}
	final := pathNoExt + "." + ext
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", err
	}
	part := final + ".part"
	if err := copyFile(path, part); err != nil {
		os.Remove(part)
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		os.Remove(part)
		return "", err
	}
	return final, nil
}

// extFromType mirrors source.Save's extension table for a bare content type.
func extFromType(ct string) string {
	mt, _, _ := mime.ParseMediaType(ct)
	switch mt {
	case "audio/flac", "audio/x-flac":
		return "flac"
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/mp4", "audio/aac", "audio/x-m4a", "audio/m4a":
		return "m4a"
	}
	if strings.HasPrefix(mt, "audio/") {
		return strings.TrimPrefix(mt, "audio/")
	}
	return ""
}

// sniffExt names the audio format by the file's magic bytes, for when the
// content type said nothing.
func sniffExt(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	b := make([]byte, 32)
	n, _ := io.ReadFull(f, b)
	b = b[:n]
	if len(b) < 16 {
		return ""
	}
	switch {
	case bytes.HasPrefix(b, []byte("fLaC")):
		return "flac"
	case bytes.HasPrefix(b, []byte("ID3")) || b[0] == 0xFF && b[1]&0xE0 == 0xE0:
		return "mp3"
	case bytes.Contains(b[:16], []byte("ftyp")):
		return "m4a"
	}
	return ""
}

func copyFile(from, to string) error {
	src, err := os.Open(from)
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(dst, src)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	return err
}

// unflatten rebuilds a SvelteKit "devalue" payload: a flat list whose numbers are indexes into it.
func unflatten(data []any) any {
	var get func(ref any) any
	get = func(ref any) any {
		f, ok := ref.(float64)
		if !ok {
			return ref
		}
		i := int(f)
		if i < 0 || i >= len(data) { // negative indexes are devalue's undefined, NaN, ...
			return nil
		}
		switch v := data[i].(type) {
		case []any:
			if len(v) > 0 {
				if _, typed := v[0].(string); typed { // a typed value like ["Date", "..."]
					if len(v) > 1 {
						return v[1]
					}
					return nil
				}
			}
			out := make([]any, len(v))
			for j, x := range v {
				out[j] = get(x)
			}
			return out
		case map[string]any:
			out := make(map[string]any, len(v))
			for k, x := range v {
				out[k] = get(x)
			}
			return out
		default:
			return v
		}
	}
	return get(0.0)
}
