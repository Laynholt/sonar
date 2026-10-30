// Package source is the contract shared by the download sites, plus the
// helpers they have in common and the Balancer that picks between them.
package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"sonar/internal/download"
)

var (
	// ErrUnavailable means the site is down or refusing us: pause it and try another.
	ErrUnavailable = errors.New("site unavailable")
	// ErrNeedsCheck means the site wants a browser check: pause it until Reset.
	ErrNeedsCheck = errors.New("needs the browser check (Settings → Downloads)")
)

// Auto lets the Balancer choose the site.
const Auto = "auto"

// Request is one track for a Source: its ids per service and where to save it. OnStatus may be nil.
type Request struct {
	Track     download.Track
	IDs       map[string]string
	PathNoExt string
	OnStatus  func(string)
}

type Source interface {
	Name() string
	// Download saves the track next to r.PathNoExt and returns the final path.
	Download(ctx context.Context, r Request) (string, error)
}

// Guard is a site's rate guard: it asks after Limit successes (0 = off); Waits are the pause choices
// in minutes, and Wait the default one.
type Guard struct {
	Limit int
	Waits []int
	Wait  int
}

type Status struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Accepts     string `json:"accepts"`
	Available   bool   `json:"available"`
	NeedsAction bool   `json:"needs_action"`
	Action      string `json:"action"` // the settings action that lifts NeedsAction, "" for none
	Reason      string `json:"reason"`
	Count       int    `json:"count"`
	Limit       int    `json:"limit"`
	Waits       []int  `json:"waits"`
	Wait        int    `json:"wait"`
	Asking      bool   `json:"asking"`
	PausedUntil int64  `json:"paused_until"` // unix ms of a guard pause, 0 for none
}

// CheckStatus returns nil for 2xx, an ErrUnavailable error for 403, 429 and 5xx, and a plain error otherwise.
func CheckStatus(site string, resp *http.Response) error {
	switch c := resp.StatusCode; {
	case c >= 200 && c < 300:
		return nil
	case c == http.StatusForbidden || c == http.StatusTooManyRequests || c >= 500:
		return fmt.Errorf("%s: HTTP %d: %w", site, c, ErrUnavailable)
	default:
		return fmt.Errorf("%s: HTTP %d", site, c)
	}
}
