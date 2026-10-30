// Command spotify is sonar's built-in Spotify source plugin.
package main

import (
	"context"
	"fmt"
	"os"

	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
	"sonar/internal/spotify"
)

func main() {
	ses := &spotify.Session{} // one login for the whole process
	s := &sdk.Server{
		Methods: map[string]sdk.Handler{
			plugin.MSourceMetadata: sdk.Typed(func(ctx context.Context, p struct{ URL string }) (any, error) {
				return ses.GetMetadata(ctx, p.URL)
			}),
			plugin.MSourceSearch: sdk.Typed(func(ctx context.Context, p struct {
				Query string
				Limit int
			}) (any, error) {
				return ses.Search(ctx, p.Query, p.Limit)
			}),
			plugin.MSourceSearchByType: sdk.Typed(func(ctx context.Context, p struct {
				Query, Type   string
				Limit, Offset int
			}) (any, error) {
				return ses.SearchByType(ctx, p.Query, p.Type, p.Limit, p.Offset)
			}),
			plugin.MSourceISRC: sdk.Typed(func(ctx context.Context, p struct{ ISRC string }) (any, error) {
				ids, err := ses.TracksByISRC(ctx, p.ISRC)
				if ids == nil {
					ids = []string{} // [] not null, as the contract says []string
				}
				return ids, err
			}),
			plugin.MSourcePreview: sdk.Typed(func(ctx context.Context, p struct{ ID string }) (any, error) {
				return spotify.PreviewURL(ctx, p.ID)
			}),
			plugin.MSourceParseLink: sdk.Typed(func(_ context.Context, p struct{ URL string }) (any, error) {
				kind, id, err := spotify.ParseLink(p.URL)
				return plugin.Link{Kind: kind, ID: id}, err
			}),
		},
	}
	s.FollowProxy("spotify")
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "spotify:", err)
		os.Exit(1)
	}
}
