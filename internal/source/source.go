// Package source is the contract shared by the download sites, plus the
// helpers they have in common and the Balancer that picks between them.
package source

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrUnavailable means the site is down or refusing us: pause it and try another.
	ErrUnavailable = errors.New("site unavailable")
	// ErrNeedsCheck means the site wants a browser check: pause it until Reset.
	ErrNeedsCheck = errors.New("needs the browser check")
)

// Auto lets the Balancer choose the site.
const Auto = "auto"

type Source interface {
	Name() string
	// Download saves the track next to pathNoExt and returns the final path. onStatus may be nil.
	Download(ctx context.Context, trackURL, pathNoExt string, onStatus func(string)) (string, error)
}

type Status struct {
	Name       string `json:"name"`
	Available  bool   `json:"available"`
	NeedsCheck bool   `json:"needs_check"`
	Reason     string `json:"reason"`
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
