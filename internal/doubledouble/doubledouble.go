// Package doubledouble is the eu.doubledouble.top download site. Its CAPTCHA token comes from the browser check.
package doubledouble

import (
	"archive/zip"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	Solver    Solver // starts a job when the stored token is missing; nil keeps the old needs-check behavior

	mu       sync.Mutex  // guards the token file
	startMu  sync.Mutex  // serializes start so a rotated token is never sent twice
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

// clearToken deletes the token only if it is still the one that was sent, so a stale
// "invalid" answer cannot remove a newer token.
func (c *Client) clearToken(sent string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, _ := os.ReadFile(c.TokenPath)
	if strings.TrimSpace(string(b)) != sent {
		return nil
	}
	if err := os.Remove(c.TokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
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
	c.startMu.Lock()
	defer c.startMu.Unlock()
	token := c.Token()
	if token == "" {
		if c.Solver == nil {
			return "", fmt.Errorf("doubledouble: %w", source.ErrNeedsCheck)
		}
		// The exchange in the browser starts the job itself: the token it
		// returns is stored for the following downloads, which use it directly.
		id, fresh, err := c.Solver.StartWithCaptcha(ctx, pageURL)
		if err != nil {
			switch {
			case errors.Is(err, source.ErrNeedsCheck): // a stuck CAPTCHA keeps the check button
				return "", err
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return "", err // the user's cancel must not pause the site
			}
			return "", fmt.Errorf("doubledouble: %w: %v", source.ErrUnavailable, err)
		}
		if err := c.SetToken(fresh); err != nil {
			return "", fmt.Errorf("doubledouble: %w", err)
		}
		return id, nil
	}
	// addmetadata=false: with it the site packs a ZIP, and sonar writes the tags itself
	q := url.Values{"url": {pageURL}, "addmetadata": {"false"}, "donotshare": {"true"}, "token": {token}}
	var a startAnswer
	if err := c.fetchJSON(ctx, c.Base+"/dl?"+q.Encode(), &a); err != nil {
		return "", err
	}
	if !a.Success || a.ID == "" {
		if strings.Contains(a.Error, "CAPTCHA") {
			if err := c.clearToken(token); err != nil {
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
			base, err := url.Parse(c.Base)
			ref, rerr := url.Parse(a.URL)
			if err != nil || rerr != nil {
				return "", false, fmt.Errorf("doubledouble: bad file URL %q", a.URL)
			}
			return base.ResolveReference(ref).String(), false, nil
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
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/zip") {
		// the site packs the track even with addmetadata=false; sonar tags the
		// file itself, so only the audio inside is needed
		path, zerr := saveZipAudio(resp, pathNoExt)
		if zerr != nil {
			if cerr := ctx.Err(); cerr != nil {
				return "", cerr
			}
			return "", fmt.Errorf("doubledouble: %w", zerr)
		}
		return path, nil
	}
	path, err := source.Save(resp, pathNoExt)
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		return "", fmt.Errorf("doubledouble: %w", err)
	}
	return path, nil
}

// saveZipAudio unpacks the archive the site sends and moves the largest audio
// entry next to pathNoExt under its extension.
func saveZipAudio(resp *http.Response, pathNoExt string) (string, error) {
	zipFile, err := os.CreateTemp("", "doubledouble-*.zip")
	if err != nil {
		return "", err
	}
	defer os.Remove(zipFile.Name())
	if _, err := io.Copy(zipFile, resp.Body); err != nil {
		zipFile.Close()
		return "", err
	}
	if err := zipFile.Close(); err != nil {
		return "", err
	}
	r, err := zip.OpenReader(zipFile.Name())
	if err != nil {
		return "", err
	}
	defer r.Close()
	var best *zip.File
	for i := range r.File {
		f := r.File[i]
		switch strings.ToLower(filepath.Ext(f.Name)) {
		case ".flac", ".mp3", ".m4a":
			if best == nil || f.UncompressedSize64 > best.UncompressedSize64 {
				best = f
			}
		}
	}
	if best == nil {
		return "", errors.New("the archive has no audio track")
	}
	ext := strings.ToLower(filepath.Ext(best.Name))
	final := pathNoExt + ext
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", err
	}
	in, err := best.Open()
	if err != nil {
		return "", err
	}
	defer in.Close()
	part := final + ".part"
	out, err := os.Create(part)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(part, final)
	}
	if err != nil {
		os.Remove(part)
		return "", err
	}
	return final, nil
}
