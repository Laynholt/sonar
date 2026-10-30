package amazon

import (
	"context"

	"sonar/internal/model"
)

func trackInfo(t *gTrack, al *gAlbum) model.TrackInfo {
	artist, _, _ := t.Artists.split()
	return model.TrackInfo{
		ID: t.ID, Title: clean(t.Title), Artist: artist,
		Album: clean(al.Title), AlbumID: al.ID, Image: pick(al.Images, "PRIMARY"), ReleaseDate: day(al.ReleaseDate),
		ISRC: t.ISRC, DurationMS: t.Duration * 1000, TrackNumber: t.TrackNumber,
		Explicit: t.ParentalSettings.HasExplicitLanguage,
	}
}

func fetchLookupTrack(ctx context.Context, q querier, id string) (*gTrack, error) {
	var out struct {
		Track *gTrack `json:"track"`
	}
	if err := q.gql(ctx, lookupTrackQuery, map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	return out.Track, nil
}

func lookupTracks(ctx context.Context, q querier, ids []string) ([]model.TrackInfo, error) {
	out := make([]model.TrackInfo, 0, len(ids))
	for _, id := range ids {
		t, err := fetchLookupTrack(ctx, q, id)
		if err != nil {
			return nil, err
		}
		if t == nil {
			continue // unknown ID
		}
		al := t.Album
		if al == nil {
			al = &gAlbum{}
		}
		out = append(out, trackInfo(t, al))
	}
	return out, nil
}

func lookupAlbum(ctx context.Context, q querier, id string) (*model.AlbumInfo, error) {
	var out struct {
		Album *gAlbum `json:"album"`
	}
	if err := q.gql(ctx, albumQuery, map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	al := out.Album
	if al == nil {
		return nil, notFound("album", id)
	}
	artist, _, _ := al.Artists.split()
	info := &model.AlbumInfo{
		ID: al.ID, Title: clean(al.Title), Artist: artist, Image: pick(al.Images, "PRIMARY"),
		ReleaseDate: day(al.ReleaseDate), Explicit: al.ParentalSettings.HasExplicitLanguage,
		Tracks: make([]model.TrackInfo, 0, len(al.Tracks)),
	}
	for i := range al.Tracks {
		info.Tracks = append(info.Tracks, trackInfo(&al.Tracks[i], al))
	}
	return info, nil
}

func previewURL(ctx context.Context, q querier, id string) (string, error) {
	t, err := fetchLookupTrack(ctx, q, id)
	if t == nil || err != nil {
		return "", err
	}
	return t.PreviewURL, nil
}

// LookupTracks returns the tracks in input order over one guest session; unknown IDs are dropped.
func LookupTracks(ctx context.Context, ids []string) ([]model.TrackInfo, error) {
	s, err := newSession(ctx)
	if err != nil {
		return nil, err
	}
	return lookupTracks(ctx, s, ids)
}

func LookupAlbum(ctx context.Context, id string) (*model.AlbumInfo, error) {
	s, err := newSession(ctx)
	if err != nil {
		return nil, err
	}
	return lookupAlbum(ctx, s, id)
}

// PreviewURL is the 30-second sample of a track, "" when it has none.
func PreviewURL(ctx context.Context, id string) (string, error) {
	s, err := newSession(ctx)
	if err != nil {
		return "", err
	}
	return previewURL(ctx, s, id)
}
