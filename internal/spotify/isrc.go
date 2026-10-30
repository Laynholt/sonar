package spotify

import (
	"context"
	"strings"
)

// TracksByISRC returns the Spotify track IDs whose ISRC matches, via the "isrc:" search filter.
func TracksByISRC(ctx context.Context, isrc string) ([]string, error) {
	return new(Session).TracksByISRC(ctx, isrc)
}

func (s *Session) TracksByISRC(ctx context.Context, isrc string) ([]string, error) {
	isrc = strings.TrimSpace(isrc)
	if isrc == "" {
		return nil, nil
	}
	res, err := s.Search(ctx, "isrc:"+isrc, 10)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(res.Tracks))
	for _, t := range res.Tracks {
		ids = append(ids, t.ID)
	}
	return ids, nil
}
