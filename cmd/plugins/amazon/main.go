// Command amazon is sonar's built-in Amazon Music source plugin.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"sonar/internal/amazon"
	"sonar/internal/httpx"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
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
	s := &sdk.Server{
		Init: func(p plugin.InitParams) error { return httpx.SetManualProxy(p.Proxy) },
		Notes: map[string]func(json.RawMessage){
			plugin.MSettingsChanged: func(raw json.RawMessage) {
				var p plugin.InitParams
				if err := json.Unmarshal(raw, &p); err != nil {
					fmt.Fprintln(os.Stderr, "amazon: bad $/settings:", err)
				} else if err := httpx.SetManualProxy(p.Proxy); err != nil {
					fmt.Fprintln(os.Stderr, "amazon: proxy:", err)
				}
			},
		},
		Methods: map[string]sdk.Handler{
			plugin.MSourceMetadata: handle(func(ctx context.Context, p struct{ URL string }) (any, error) {
				return amazon.GetMetadata(ctx, p.URL)
			}),
			// The UI search; limit is not used, as today.
			plugin.MSourceSearch: handle(func(ctx context.Context, p struct{ Query string }) (any, error) {
				return amazon.SearchCatalog(ctx, p.Query)
			}),
			// The search the matcher uses.
			plugin.MSourceFind: handle(func(ctx context.Context, p struct{ Query string }) (any, error) {
				return amazon.Search(ctx, p.Query)
			}),
			plugin.MSourceTracks: handle(func(ctx context.Context, p struct{ IDs []string }) (any, error) {
				return amazon.LookupTracks(ctx, p.IDs)
			}),
			plugin.MSourceAlbum: handle(func(ctx context.Context, p struct{ ID string }) (any, error) {
				return amazon.LookupAlbum(ctx, p.ID)
			}),
			plugin.MSourcePreview: handle(func(ctx context.Context, p struct{ ID string }) (any, error) {
				return amazon.PreviewURL(ctx, p.ID)
			}),
			plugin.MSourceParseLink: handle(func(_ context.Context, p struct{ URL string }) (any, error) {
				l, err := amazon.ParseLink(p.URL)
				return plugin.Link{Kind: l.Kind, ID: l.ID}, err
			}),
		},
	}
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "amazon:", err)
		os.Exit(1)
	}
}
