// Package dl is the download handler shared by the built-in downloader plugins. It stays out of
// internal/plugin/sdk so third-party plugins do not import amazon, source and download through it.
package dl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"sonar/internal/amazon"
	"sonar/internal/download"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
	"sonar/internal/source"
)

// Downloader is a download site client, such as lucida or doubledouble.
type Downloader interface {
	Download(ctx context.Context, trackURL, pathNoExt string, onStatus func(string)) (string, error)
}

// Handler serves plugin.MDownload for a downloader that accepts "amazon". get runs per request,
// so a plugin can build its client in Init. Byte progress reaches $/progress through download.WithProgress
// on the ctx; the site's status texts go out as $/progress status. Site errors become the contract's codes;
// needs gives a needs-action error its data (nil: a plain browser check).
func Handler(get func() (Downloader, error), needs func() plugin.NeedsAction) sdk.Handler {
	if needs == nil {
		needs = func() plugin.NeedsAction {
			return plugin.NeedsAction{Message: "Needs the browser check", Action: "check"}
		}
	}
	return func(ctx context.Context, raw json.RawMessage, c *sdk.Call) (any, error) {
		var p plugin.DownloadParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
		d, err := get()
		if err != nil {
			return nil, err
		}
		u, err := amazon.TrackURL(p.IDs["amazon"])
		if err != nil {
			return nil, err
		}
		var done, total int64 // both callbacks run in the goroutine of d.Download
		ctx = download.WithProgress(ctx, func(n, size int64) {
			done, total = n, size
			c.Progress(n, size, "")
		})
		path, err := d.Download(ctx, u, p.PathNoExt, func(status string) { c.Progress(done, total, status) })
		switch {
		case errors.Is(err, source.ErrUnavailable):
			return nil, sdk.Fail(plugin.CodeUnavailable, err.Error(), nil)
		case errors.Is(err, source.ErrNeedsCheck):
			return nil, sdk.Fail(plugin.CodeNeedsAction, err.Error(), needs())
		case err != nil:
			return nil, err
		}
		return plugin.DownloadResult{Path: path}, nil
	}
}
