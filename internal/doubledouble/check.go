package doubledouble

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"sonar/internal/browser"
	"sonar/internal/source"
)

// ErrCheckClosed means the visible check window was closed before the check was passed.
var ErrCheckClosed = errors.New("the check window was closed before the check was passed")

// checkTrackURL is the page the visible check opens, the same one the check has always used.
const checkTrackURL = "https://music.amazon.com/tracks/B00A3SGXAA"

// autoWait budgets the invisible attempt: the challenge should clear and the
// Turnstile widget should solve itself in a warmed-up headless browser.
const autoWait = 45 * time.Second

// Solver starts a download job when no stored token exists: it solves the site's
// Turnstile challenge in the browser and exchanges the token, exactly like the
// site's own page does.
type Solver interface {
	StartWithCaptcha(ctx context.Context, pageURL string) (jobID, captchaToken string, err error)
}

// BrowserSolver implements Solver with the browser client.
type BrowserSolver struct {
	C  *Client // for the site Base (tests point it at a fake server)
	BC *browser.Client
}

// StartWithCaptcha is the invisible attempt: headless browser, self-solving widget.
func (s BrowserSolver) StartWithCaptcha(ctx context.Context, pageURL string) (string, string, error) {
	page, err := s.BC.Page(ctx, s.C.Base)
	if err != nil {
		return "", "", solverErr(err)
	}
	return s.startWithCaptcha(ctx, page, pageURL, autoWait)
}

// CheckVisible is the manual fallback: the browser relaunches with a window, the
// user passes the check there, and the returned token gets stored by the caller.
func (s BrowserSolver) CheckVisible(ctx context.Context, trackURL string) (string, error) {
	s.BC.SetVisible(true)
	defer s.BC.SetVisible(false)
	page, err := s.BC.PageFor(ctx, s.C.Base)
	if err != nil {
		return "", solverErr(err)
	}
	if err := s.BC.WaitChallenge(ctx, page, browser.VisibleWait); err != nil {
		return "", solverErr(err)
	}
	_, tok, err := s.startWithCaptcha(ctx, page, trackURL, browser.VisibleWait)
	return tok, err
}

func (s BrowserSolver) startWithCaptcha(ctx context.Context, page *browser.Page, pageURL string, wait time.Duration) (string, string, error) {
	status, body, err := s.BC.Fetch(ctx, page, http.MethodGet, s.C.Base+"/captcha/config?url="+url.QueryEscape(pageURL), nil, nil)
	if err != nil {
		return "", "", solverErr(err)
	}
	if status != http.StatusOK {
		return "", "", fmt.Errorf("doubledouble: %w: captcha config: HTTP %d", source.ErrUnavailable, status)
	}
	var cfg struct {
		Active  bool   `json:"active"`
		Type    string `json:"type"`
		Sitekey string `json:"sitekey"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		return "", "", fmt.Errorf("doubledouble: %w: captcha config is not JSON: %v", source.ErrUnavailable, err)
	}
	if !cfg.Active {
		return "", "", fmt.Errorf("doubledouble: %w: captcha is not active (site changed?)", source.ErrUnavailable)
	}
	if cfg.Type != "turnstile" || cfg.Sitekey == "" {
		return "", "", fmt.Errorf("doubledouble: unknown captcha type %q", cfg.Type)
	}
	tok, err := s.BC.SolveTurnstile(ctx, page, cfg.Sitekey, wait)
	if err != nil {
		return "", "", solverErr(err)
	}
	q := url.Values{"url": {pageURL}, "addmetadata": {"false"}, "donotshare": {"true"}, "captcha": {tok}}
	status, body, err = s.BC.Fetch(ctx, page, http.MethodGet, s.C.Base+"/dl?"+q.Encode(), nil, nil)
	if err != nil {
		return "", "", solverErr(err)
	}
	if status != http.StatusOK {
		return "", "", fmt.Errorf("doubledouble: %w: captcha exchange: HTTP %d", source.ErrUnavailable, status)
	}
	var a startAnswer
	if err := json.Unmarshal(body, &a); err != nil {
		return "", "", fmt.Errorf("doubledouble: %w: captcha exchange is not JSON: %v", source.ErrUnavailable, err)
	}
	if !a.Success || a.ID == "" || a.CaptchaToken == "" {
		if a.Error == "" {
			a.Error = "unexpected answer (site changed?)"
		}
		return "", "", fmt.Errorf("doubledouble: %s", a.Error)
	}
	return a.ID, a.CaptchaToken, nil
}

func solverErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, browser.ErrChallengeStuck), errors.Is(err, browser.ErrTurnstileFailed):
		return fmt.Errorf("doubledouble: %w: the CAPTCHA did not solve itself, press the check button", source.ErrNeedsCheck)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	default:
		return fmt.Errorf("doubledouble: %w: %v", source.ErrUnavailable, err)
	}
}

// CheckVisibleToken runs the visible check and stores the token it produces.
func (c *Client) CheckVisibleToken(ctx context.Context, bc *browser.Client) error {
	tok, err := BrowserSolver{C: c, BC: bc}.CheckVisible(ctx, checkTrackURL)
	if err != nil {
		if errors.Is(err, browser.ErrBrowserGone) {
			return ErrCheckClosed
		}
		return err
	}
	if err := c.SetToken(tok); err != nil {
		return fmt.Errorf("doubledouble: saving the check token: %w", err)
	}
	return nil
}
