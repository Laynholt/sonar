// Package doubledouble is the eu.doubledouble.top download site. Its CAPTCHA token comes from the browser check.
package doubledouble

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sonar/internal/httpx"
	"sonar/internal/source"
)

const maxKickbacks = 2

type Client struct {
	HTTP      *http.Client
	Base      string
	PollEvery time.Duration
	TokenPath string

	mu       sync.Mutex  // guards the token file
	checking atomic.Bool // set by the browser check so only one runs at a time
}

func New(tokenPath string) *Client {
	return &Client{
		HTTP:      &http.Client{Transport: httpx.Client().Transport},
		Base:      "https://eu.doubledouble.top",
		PollEvery: 2 * time.Second,
		TokenPath: tokenPath,
	}
}

func (c *Client) Name() string { return "doubledouble" }

// Token returns the stored CAPTCHA token, or "".
func (c *Client) Token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := os.ReadFile(c.TokenPath)
	return strings.TrimSpace(string(b))
}

// SetToken stores the token, or deletes it when t is empty.
func (c *Client) SetToken(t string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t == "" {
		if err := os.Remove(c.TokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return os.WriteFile(c.TokenPath, []byte(t), 0o600)
}

// get sends a GET and returns the response with a 2xx status; the caller closes its body.
func (c *Client) get(ctx context.Context, u string) (*http.Response, error) {
	resp, err := c.getAny(ctx, u)
	if err != nil {
		return nil, err
	}
	if err := source.CheckStatus("doubledouble", resp); err != nil {
		resp.Body.Close()
		return nil, err
	}
	return resp, nil
}

func (c *Client) getAny(ctx context.Context, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		return nil, fmt.Errorf("doubledouble: %w: %v", source.ErrUnavailable, err)
	}
	return resp, nil
}

// fetchJSON decodes a JSON answer into out. Network errors, 403/429/5xx and non-JSON answers
// (a Cloudflare page) wrap source.ErrUnavailable; other 4xx answers are kept if they are JSON,
// since the site reports its CAPTCHA errors that way.
func (c *Client) fetchJSON(ctx context.Context, u string, out any) error {
	resp, err := c.getAny(ctx, u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	statusErr := source.CheckStatus("doubledouble", resp)
	if errors.Is(statusErr, source.ErrUnavailable) {
		return statusErr
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		if statusErr != nil {
			return statusErr
		}
		return fmt.Errorf("doubledouble: %w: answer is not JSON: %v", source.ErrUnavailable, err)
	}
	return nil
}

type startAnswer struct {
	Success      bool   `json:"success"`
	ID           string `json:"id"`
	Error        string `json:"error"`
	CaptchaToken string `json:"captchaToken"`
}

type pollAnswer struct {
	Status         string `json:"status"`
	FriendlyStatus string `json:"friendlyStatus"`
	Percent        any    `json:"percent"`
	Error          string `json:"error"`
	URL            string `json:"url"`
	Message        string `json:"message"`
	Kickback       bool   `json:"kickback"`
}

// start asks the site to prepare the track and returns the job id.
func (c *Client) start(ctx context.Context, pageURL string) (string, error) {
	token := c.Token()
	if token == "" {
		return "", fmt.Errorf("doubledouble: %w", source.ErrNeedsCheck)
	}
	q := url.Values{"url": {pageURL}, "addmetadata": {"true"}, "donotshare": {"true"}, "token": {token}}
	var a startAnswer
	if err := c.fetchJSON(ctx, c.Base+"/dl?"+q.Encode(), &a); err != nil {
		return "", err
	}
	if !a.Success || a.ID == "" {
		if strings.Contains(a.Error, "CAPTCHA") {
			if err := c.SetToken(""); err != nil {
				return "", fmt.Errorf("doubledouble: %w", err)
			}
			return "", fmt.Errorf("doubledouble: %w: %s", source.ErrNeedsCheck, a.Error)
		}
		if a.Error == "" {
			a.Error = "unexpected answer (site changed?)"
		}
		return "", fmt.Errorf("doubledouble: %s", a.Error)
	}
	if a.CaptchaToken != "" { // the site rotates the token on every request
		if err := c.SetToken(a.CaptchaToken); err != nil {
			return "", fmt.Errorf("doubledouble: %w", err)
		}
	}
	return a.ID, nil
}

// wait polls the job until it is done. kickback is true when the site asks to restart the request.
func (c *Client) wait(ctx context.Context, id string, onStatus func(string)) (fileURL string, kickback bool, err error) {
	for {
		var a pollAnswer
		if err := c.fetchJSON(ctx, c.Base+"/dl/"+url.PathEscape(id), &a); err != nil {
			return "", false, err
		}
		switch a.Status {
		case "done":
			if a.URL == "" {
				return "", false, errors.New("doubledouble: finished without a file URL")
			}
			if rest, ok := strings.CutPrefix(a.URL, "./"); ok {
				return c.Base + "/" + rest, false, nil
			}
			return a.URL, false, nil
		case "error":
			if a.Kickback {
				return "", true, nil
			}
			if a.Message == "" {
				a.Message = "download failed"
			}
			return "", false, fmt.Errorf("doubledouble: %s", a.Message)
		}
		if a.Status == "" { // e.g. {"success":false,"error":"job not found"}
			m := cmp.Or(a.Error, a.Message, "unexpected poll answer")
			return "", false, fmt.Errorf("doubledouble: %s", m)
		}
		msg := a.FriendlyStatus
		if msg == "" {
			msg = a.Status
		}
		if p, ok := a.Percent.(float64); ok { // the site may send a string
			msg += fmt.Sprintf(" (%.0f%%)", math.Round(p))
		}
		onStatus(msg)
		select {
		case <-ctx.Done():
			return "", false, ctx.Err()
		case <-time.After(c.PollEvery):
		}
	}
}

func (c *Client) Download(ctx context.Context, trackURL, pathNoExt string, onStatus func(string)) (string, error) {
	if onStatus == nil {
		onStatus = func(string) {}
	}
	pageURL := trackURL
	if u, err := url.Parse(trackURL); err == nil && u.RawQuery == "" {
		pageURL += "?musicTerritory=US"
	}

	var fileURL string
	for attempt := 0; ; attempt++ {
		if attempt > maxKickbacks {
			return "", errors.New("doubledouble: server kept restarting the request")
		}
		id, err := c.start(ctx, pageURL)
		if err != nil {
			return "", err
		}
		var kick bool
		if fileURL, kick, err = c.wait(ctx, id, onStatus); err != nil {
			return "", err
		}
		if !kick {
			break
		}
	}

	resp, err := c.get(ctx, fileURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	path, err := source.Save(resp, pathNoExt)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		return "", fmt.Errorf("doubledouble: %w", err)
	}
	return path, nil
}
