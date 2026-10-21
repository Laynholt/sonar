// Package lucida is the lucida.to download site: a Go port of lucida.py for single tracks.
package lucida

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sonar/internal/httpx"
	"sonar/internal/source"
)

type Client struct {
	HTTP      *http.Client
	Base      string
	PollEvery time.Duration
}

func New() *Client {
	return &Client{
		HTTP:      &http.Client{Transport: httpx.Client().Transport},
		Base:      "https://lucida.to",
		PollEvery: time.Second,
	}
}

func (c *Client) Name() string { return "lucida" }

func (c *Client) api(path string) string { return c.Base + "/api/load?url=" + url.QueryEscape(path) }

// fetchJSON sends the request and decodes a JSON answer into out. Network errors, refusals
// and non-JSON answers (a Cloudflare page) wrap source.ErrUnavailable.
func (c *Client) fetchJSON(ctx context.Context, method, u string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	resp, err := c.do(ctx, method, u, rd)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("lucida: %w: answer is not JSON: %v", source.ErrUnavailable, err)
	}
	return nil
}

// do returns a 2xx response; the caller closes its body.
func (c *Client) do(ctx context.Context, method, u string, body *bytes.Reader) (*http.Response, error) {
	var rd io.Reader = http.NoBody
	if body != nil {
		rd = body
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		return nil, fmt.Errorf("lucida: %w: %v", source.ErrUnavailable, err)
	}
	if err := source.CheckStatus("lucida", resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
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
		"url": page["download"], "metadata": true, "compat": false, "private": true, "handoff": true,
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

	resp, err := c.do(ctx, http.MethodGet, c.api(req+"/download")+force+"&redirect=true", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	path, err := source.Save(resp, pathNoExt)
	if err != nil {
		return "", fmt.Errorf("lucida: %w", err)
	}
	return path, nil
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
