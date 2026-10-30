package resolve

import (
	"context"
	"fmt"

	"sonar/internal/amazon"
	"sonar/internal/match"
	"sonar/internal/model"
	"sonar/internal/spotify"
)

type live struct{}

// Live is the Finders of the in-process packages: Amazon's guest API and Spotify's ISRC search,
// so spotify to amazon only. shim until Task 13, which switches to PluginFinders and deletes this file.
func Live() Finders {
	return func(from, to string) (Finder, error) {
		if from != "spotify" || to != "amazon" {
			return nil, fmt.Errorf("no matcher from %s to %s", from, to)
		}
		return live{}, nil
	}
}

func (live) Search(ctx context.Context, q string) ([]match.Item, error) {
	return amazon.Search(ctx, q)
}

func (live) LookupTracks(ctx context.Context, ids []string) ([]model.TrackInfo, error) {
	return amazon.LookupTracks(ctx, ids)
}

func (live) LookupAlbum(ctx context.Context, id string) (*model.AlbumInfo, error) {
	return amazon.LookupAlbum(ctx, id)
}

func (live) OriginIDsByISRC(ctx context.Context, isrc string) ([]string, error) {
	return spotify.TracksByISRC(ctx, isrc)
}
