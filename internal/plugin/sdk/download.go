package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"sonar/internal/amazon"
	"sonar/internal/download"
	"sonar/internal/plugin"
	"sonar/internal/source"
)

// Downloader is a download site client, such as lucida or doubledouble.
type Downloader interface {
	Download(ctx context.Context, trackURL, pathNoExt string, onStatus func(string)) (string, error)
}

// DownloadHandler serves plugin.MDownload for a downloader that accepts "amazon". get runs per request,
// so a plugin can build its client in Init. Byte progress reaches $/progress through download.WithProgress
// on the ctx; the site's status texts go out as $/progress status. Site errors become the contract's codes.
func DownloadHandler(get func() (Downloader, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage, c *Call) (any, error) {
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
			return nil, Fail(plugin.CodeUnavailable, err.Error(), nil)
		case errors.Is(err, source.ErrNeedsCheck):
			return nil, Fail(plugin.CodeNeedsAction, err.Error(), plugin.NeedsAction{Message: "Needs the browser check", Action: "check"})
		case err != nil:
			return nil, err
		}
		return plugin.DownloadResult{Path: path}, nil
	}
}
