// Command spotify is sonar's built-in Spotify source plugin.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"sonar/internal/httpx"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
	"sonar/internal/spotify"
)

// handle decodes a request's params into P before calling f.
func handle[P any](f func(context.Context, P) (any, error)) sdk.Handler {
	return func(ctx context.Context, raw json.RawMessage, _ *sdk.Call) (any, error) {
		var p P
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
		return f(ctx, p)
	}
}

func main() {
	ses := &spotify.Session{} // one login for the whole process
	s := &sdk.Server{
		Init: func(p plugin.InitParams) error { return httpx.SetManualProxy(p.Proxy) },
		Notes: map[string]func(json.RawMessage){
			plugin.MSettingsChanged: func(raw json.RawMessage) {
				var p plugin.InitParams
				if err := json.Unmarshal(raw, &p); err != nil {
					fmt.Fprintln(os.Stderr, "spotify: bad $/settings:", err)
				} else if err := httpx.SetManualProxy(p.Proxy); err != nil {
					fmt.Fprintln(os.Stderr, "spotify: proxy:", err)
				}
			},
		},
		Methods: map[string]sdk.Handler{
			plugin.MSourceMetadata: handle(func(ctx context.Context, p struct{ URL string }) (any, error) {
				return ses.GetMetadata(ctx, p.URL)
			}),
			plugin.MSourceSearch: handle(func(ctx context.Context, p struct {
				Query string
				Limit int
			}) (any, error) {
				return ses.Search(ctx, p.Query, p.Limit)
			}),
			plugin.MSourceSearchByType: handle(func(ctx context.Context, p struct {
				Query, Type   string
				Limit, Offset int
			}) (any, error) {
				return ses.SearchByType(ctx, p.Query, p.Type, p.Limit, p.Offset)
			}),
			plugin.MSourceISRC: handle(func(ctx context.Context, p struct{ ISRC string }) (any, error) {
				ids, err := ses.TracksByISRC(ctx, p.ISRC)
				if ids == nil {
					ids = []string{} // [] not null, as the contract says []string
				}
				return ids, err
			}),
			plugin.MSourcePreview: handle(func(ctx context.Context, p struct{ ID string }) (any, error) {
				return spotify.PreviewURL(ctx, p.ID)
			}),
			plugin.MSourceParseLink: handle(func(_ context.Context, p struct{ URL string }) (any, error) {
				kind, id, err := spotify.ParseLink(p.URL)
				return plugin.Link{Kind: kind, ID: id}, err
			}),
		},
	}
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "spotify:", err)
		os.Exit(1)
	}
}
