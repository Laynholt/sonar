// Command amazon is sonar's built-in Amazon Music source plugin.
package main

import (
	"context"
	"fmt"
	"os"

	"sonar/internal/amazon"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

func main() {
	s := &sdk.Server{
		Methods: map[string]sdk.Handler{
			plugin.MSourceMetadata: sdk.Typed(func(ctx context.Context, p struct{ URL string }) (any, error) {
				return amazon.GetMetadata(ctx, p.URL)
			}),
			// The UI search; limit is not used, as today.
			plugin.MSourceSearch: sdk.Typed(func(ctx context.Context, p struct{ Query string }) (any, error) {
				return amazon.SearchCatalog(ctx, p.Query)
			}),
			// The search the matcher uses.
			plugin.MSourceFind: sdk.Typed(func(ctx context.Context, p struct{ Query string }) (any, error) {
				return amazon.Search(ctx, p.Query)
			}),
			plugin.MSourceTracks: sdk.Typed(func(ctx context.Context, p struct{ IDs []string }) (any, error) {
				return amazon.LookupTracks(ctx, p.IDs)
			}),
			plugin.MSourceAlbum: sdk.Typed(func(ctx context.Context, p struct{ ID string }) (any, error) {
				return amazon.LookupAlbum(ctx, p.ID)
			}),
			plugin.MSourcePreview: sdk.Typed(func(ctx context.Context, p struct{ ID string }) (any, error) {
				return amazon.PreviewURL(ctx, p.ID)
			}),
			plugin.MSourceParseLink: sdk.Typed(func(_ context.Context, p struct{ URL string }) (any, error) {
				l, err := amazon.ParseLink(p.URL)
				return plugin.Link{Kind: l.Kind, ID: l.ID}, err
			}),
		},
	}
	s.FollowProxy("amazon")
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "amazon:", err)
		os.Exit(1)
	}
}
