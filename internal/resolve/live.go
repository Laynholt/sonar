package resolve

import (
	"context"

	"sonar/internal/amazon"
	"sonar/internal/match"
	"sonar/internal/model"
	"sonar/internal/spotify"
)

type live struct{}

// Live is the Finder backed by Amazon's guest API and Spotify's ISRC search.
func Live() Finder { return live{} }

func (live) Search(ctx context.Context, q string) ([]match.Item, error) {
	return amazon.Search(ctx, q)
}

func (live) LookupTracks(ctx context.Context, ids []string) ([]model.TrackInfo, error) {
	return amazon.LookupTracks(ctx, ids)
}

func (live) LookupAlbum(ctx context.Context, id string) (*model.AlbumInfo, error) {
	return amazon.LookupAlbum(ctx, id)
}

func (live) SpotifyIDsByISRC(ctx context.Context, isrc string) ([]string, error) {
	return spotify.TracksByISRC(ctx, isrc)
}
