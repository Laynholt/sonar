package amazon

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"sonar/internal/model"
)

const (
	imgQ = `images { url imageType width height }`
	artQ = `contributingArtists { edges { node { id name } role } }`
	expQ = `parentalSettings { hasExplicitLanguage }`

	trackQuery    = `query ($id: String!) { track(id: $id) { id title duration isrc trackNumber ` + expQ + ` ` + artQ + ` album { id title releaseDate copyright trackCount ` + imgQ + ` ` + artQ + ` } } }`
	albumQuery    = `query ($id: String!) { album(id: $id) { id title releaseDate copyright trackCount ` + imgQ + ` ` + expQ + ` ` + artQ + ` tracks { id title duration trackNumber ` + expQ + ` ` + artQ + ` } } }`
	playlistQuery = `query ($id: String!, $after: String) { playlist(id: $id) { id title description trackCount curator { name } followers { totalCount } ` + imgQ + ` tracks(first: 100, after: $after) { totalCount pageInfo { hasNextPage token } edges { node { id title duration ` + expQ + ` album { id title ` + imgQ + ` } ` + artQ + ` } } } } }`
	artistQuery   = `query ($id: String!, $cursor: String) { artist(id: $id) { id name ` + imgQ + ` albums(limit: 100, cursor: $cursor) { pageInfo { hasNextPage token } edges { node { id title releaseDate trackCount format ` + imgQ + ` } } } } }`
	albumsQuery   = `query ($ids: [String!]!) { albums(ids: $ids) { id title releaseDate trackCount ` + imgQ + ` ` + artQ + ` tracks { id title duration trackNumber ` + expQ + ` ` + artQ + ` } } }`

	maxPages = 100
	batchMax = 100
)

type gImage struct {
	URL       string `json:"url"`
	ImageType string `json:"imageType"`
	Width     int    `json:"width"`
}

type gArtists struct {
	Edges []struct {
		Node struct{ ID, Name string } `json:"node"`
		Role string                    `json:"role"`
	} `json:"edges"`
}

type gTrack struct {
	ID               string `json:"id"`
	Title            string `json:"title"`
	Duration         int    `json:"duration"`
	TrackNumber      int    `json:"trackNumber"`
	ParentalSettings struct {
		HasExplicitLanguage bool `json:"hasExplicitLanguage"`
	} `json:"parentalSettings"`
	Artists gArtists `json:"contributingArtists"`
	Album   *gAlbum  `json:"album"`
}

// gAlbum serves every query that returns an album; unrequested fields stay zero.
type gAlbum struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	ReleaseDate string   `json:"releaseDate"`
	Copyright   string   `json:"copyright"`
	TrackCount  int      `json:"trackCount"`
	Format      string   `json:"format"`
	Images      []gImage `json:"images"`
	Artists     gArtists `json:"contributingArtists"`
	Tracks      []gTrack `json:"tracks"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	Token       string `json:"token"`
}

type gPlaylist struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	TrackCount  int    `json:"trackCount"`
	Curator     struct {
		Name string `json:"name"`
	} `json:"curator"`
	Followers struct {
		TotalCount int `json:"totalCount"`
	} `json:"followers"`
	Images []gImage `json:"images"`
	Tracks struct {
		PageInfo pageInfo `json:"pageInfo"`
		Edges    []struct {
			Node gTrack `json:"node"`
		} `json:"edges"`
	} `json:"tracks"`
}

type gArtist struct {
	Name   string   `json:"name"`
	Images []gImage `json:"images"`
	Albums struct {
		PageInfo pageInfo `json:"pageInfo"`
		Edges    []struct {
			Node gAlbum `json:"node"`
		} `json:"edges"`
	} `json:"albums"`
}

func notFound(kind, id string) error { return fmt.Errorf("amazon: %s %s not found", kind, id) }

// canonical builds URLs from IDs; API URLs carry ?ref= noise.
func canonical(kind, id string) string { return "https://music.amazon.com/" + kind + "/" + id }

// clean drops the suffix the E badge already shows.
func clean(title string) string {
	return strings.TrimSuffix(strings.TrimSuffix(title, " [Explicit]"), " [Clean]")
}

// pick returns the widest image of the wanted type, else the widest of any type.
func pick(imgs []gImage, typ string) string {
	var typed, widest gImage
	for _, im := range imgs {
		if im.ImageType == typ && im.Width > typed.Width {
			typed = im
		}
		if im.Width > widest.Width {
			widest = im
		}
	}
	if typed.URL != "" {
		return typed.URL
	}
	return widest.URL
}

func day(s string) string { d, _, _ := strings.Cut(s, "T"); return d }

func albumType(format string) string {
	switch format {
	case "ALBUM":
		return "album"
	case "SINGLE":
		return "single"
	case "EXTENDED_PLAY":
		return "ep"
	}
	return strings.ToLower(format)
}

// split returns the joined names, the first PRIMARY artist's name, and the artist list.
func (a gArtists) split() (joined, primary string, data []model.ArtistSimple) {
	names := make([]string, 0, len(a.Edges))
	for _, e := range a.Edges {
		names = append(names, e.Node.Name)
		data = append(data, model.ArtistSimple{ID: e.Node.ID, Name: e.Node.Name, ExternalURL: canonical("artists", e.Node.ID)})
		if primary == "" && e.Role == "PRIMARY" {
			primary = e.Node.Name
		}
	}
	return strings.Join(names, ", "), primary, data
}

// albumTrack maps a track; al supplies album-level fields (cover, date, album artist).
func albumTrack(t *gTrack, al *gAlbum) model.AlbumTrackMetadata {
	artists, _, data := t.Artists.split()
	_, albumArtist, _ := al.Artists.split()
	m := model.AlbumTrackMetadata{
		Service:     model.Amazon,
		ID:          t.ID,
		Artists:     artists,
		Name:        clean(t.Title),
		AlbumName:   clean(al.Title),
		AlbumArtist: albumArtist,
		DurationMS:  t.Duration * 1000,
		Images:      pick(al.Images, "PRIMARY"),
		ReleaseDate: day(al.ReleaseDate),
		TrackNumber: t.TrackNumber,
		TotalTracks: al.TrackCount,
		ExternalURL: canonical("tracks", t.ID),
		AlbumID:     al.ID,
		AlbumURL:    canonical("albums", al.ID),
		ArtistsData: data,
		IsExplicit:  t.ParentalSettings.HasExplicitLanguage,
	}
	if len(data) > 0 {
		m.ArtistID, m.ArtistURL = data[0].ID, data[0].ExternalURL
	}
	return m
}

func fetchTrack(ctx context.Context, q querier, id string) (*model.TrackResponse, error) {
	var out struct {
		Track *gTrack `json:"track"`
	}
	if err := q.gql(ctx, trackQuery, map[string]any{"id": id}, &out); err != nil {
		return nil, err
	}
	t := out.Track
	if t == nil || t.Album == nil {
		return nil, notFound("track", id)
	}
	a := albumTrack(t, t.Album)
	return &model.TrackResponse{Service: model.Amazon, Track: model.TrackMetadata{
		Service: a.Service, ID: a.ID, Artists: a.Artists, Name: a.Name, AlbumName: a.AlbumName,
		AlbumArtist: a.AlbumArtist, DurationMS: a.DurationMS, Images: a.Images, ReleaseDate: a.ReleaseDate,
		TrackNumber: a.TrackNumber, TotalTracks: a.TotalTracks, ExternalURL: a.ExternalURL,
		Copyright: t.Album.Copyright, IsExplicit: a.IsExplicit, AlbumID: a.AlbumID, AlbumURL: a.AlbumURL,
		ArtistID: a.ArtistID, ArtistURL: a.ArtistURL, ArtistsData: a.ArtistsData,
	}}, nil
}

func fetchAlbum(ctx context.Context, q querier, id string) (*model.AlbumResponsePayload, error) {
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
	artists, _, data := al.Artists.split()
	info := model.AlbumInfoMetadata{
		TotalTracks: al.TrackCount,
		Name:        clean(al.Title),
		ReleaseDate: day(al.ReleaseDate),
		Artists:     artists,
		Images:      pick(al.Images, "PRIMARY"),
		ExternalURL: canonical("albums", id),
	}
	if len(data) > 0 {
		info.ArtistID, info.ArtistURL = data[0].ID, data[0].ExternalURL
	}
	list := make([]model.AlbumTrackMetadata, 0, len(al.Tracks))
	for i := range al.Tracks {
		list = append(list, albumTrack(&al.Tracks[i], al))
	}
	return &model.AlbumResponsePayload{Service: model.Amazon, AlbumInfo: info, TrackList: list}, nil
}

func fetchPlaylist(ctx context.Context, q querier, id string) (*model.PlaylistResponsePayload, error) {
	var first *gPlaylist
	list := []model.AlbumTrackMetadata{}
	after := ""
	// shortcut: stops at 10 000 tracks, raise if a real playlist hits it
	for range maxPages {
		vars := map[string]any{"id": id}
		if after != "" {
			vars["after"] = after
		}
		var out struct {
			Playlist *gPlaylist `json:"playlist"`
		}
		if err := q.gql(ctx, playlistQuery, vars, &out); err != nil {
			return nil, err
		}
		p := out.Playlist
		if p == nil {
			return nil, notFound("playlist", id)
		}
		if first == nil {
			first = p
		}
		for i := range p.Tracks.Edges {
			t := &p.Tracks.Edges[i].Node
			al := t.Album
			if al == nil {
				al = &gAlbum{}
			}
			list = append(list, albumTrack(t, al))
		}
		if !p.Tracks.PageInfo.HasNextPage || p.Tracks.PageInfo.Token == "" {
			break
		}
		after = p.Tracks.PageInfo.Token
	}
	var info model.PlaylistInfoMetadata
	info.Tracks.Total = first.TrackCount
	info.Followers.Total = first.Followers.TotalCount
	info.Owner.Name = clean(first.Title)
	info.Owner.DisplayName = first.Curator.Name
	info.Cover = pick(first.Images, "PRIMARY")
	info.Description = first.Description
	info.ExternalURL = canonical("playlists", id)
	return &model.PlaylistResponsePayload{Service: model.Amazon, PlaylistInfo: info, TrackList: list}, nil
}

func fetchArtist(ctx context.Context, q querier, id string) (*model.ArtistDiscographyPayload, error) {
	var first *gArtist
	var albums []gAlbum
	cursor := ""
	// shortcut: stops at 10 000 albums, raise if a real artist hits it
	for range maxPages {
		vars := map[string]any{"id": id}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		var out struct {
			Artist *gArtist `json:"artist"`
		}
		if err := q.gql(ctx, artistQuery, vars, &out); err != nil {
			return nil, err
		}
		a := out.Artist
		if a == nil {
			return nil, notFound("artist", id)
		}
		if first == nil {
			first = a
		}
		for _, e := range a.Albums.Edges {
			albums = append(albums, e.Node)
		}
		if !a.Albums.PageInfo.HasNextPage || a.Albums.PageInfo.Token == "" {
			break
		}
		cursor = a.Albums.PageInfo.Token
	}

	ids := make([]string, len(albums))
	for i, al := range albums {
		ids[i] = al.ID
	}
	full := make(map[string]*gAlbum, len(ids))
	for chunk := range slices.Chunk(ids, batchMax) {
		var out struct {
			Albums []gAlbum `json:"albums"`
		}
		if err := q.gql(ctx, albumsQuery, map[string]any{"ids": chunk}, &out); err != nil {
			return nil, err
		}
		for i := range out.Albums {
			full[out.Albums[i].ID] = &out.Albums[i]
		}
	}

	list := make([]model.DiscographyAlbumMetadata, 0, len(albums))
	tracks := []model.AlbumTrackMetadata{}
	for _, al := range albums {
		list = append(list, model.DiscographyAlbumMetadata{
			ID:          al.ID,
			Name:        clean(al.Title),
			AlbumType:   albumType(al.Format),
			ReleaseDate: day(al.ReleaseDate),
			TotalTracks: al.TrackCount,
			Artists:     first.Name,
			Images:      pick(al.Images, "PRIMARY"),
			ExternalURL: canonical("albums", al.ID),
		})
		if f := full[al.ID]; f != nil {
			for i := range f.Tracks {
				tracks = append(tracks, albumTrack(&f.Tracks[i], f))
			}
		}
	}
	return &model.ArtistDiscographyPayload{
		Service: model.Amazon,
		ArtistInfo: model.ArtistInfoMetadata{
			Name:            first.Name,
			Images:          pick(first.Images, "PROFILE"),
			Header:          pick(first.Images, "BACKGROUND"),
			ExternalURL:     canonical("artists", id),
			DiscographyType: "all",
			TotalAlbums:     len(albums),
		},
		AlbumList: list,
		TrackList: tracks,
	}, nil
}

// GetMetadata returns *model.TrackResponse, *model.AlbumResponsePayload,
// *model.PlaylistResponsePayload or *model.ArtistDiscographyPayload.
func GetMetadata(ctx context.Context, rawURL string) (any, error) {
	l, err := ParseLink(rawURL) // before newSession: bad links must fail offline
	if err != nil {
		return nil, err
	}
	s, err := newSession(ctx)
	if err != nil {
		return nil, err
	}
	var r any
	switch l.Kind {
	case "track":
		r, err = fetchTrack(ctx, s, l.ID)
	case "album":
		r, err = fetchAlbum(ctx, s, l.ID)
	case "playlist":
		r, err = fetchPlaylist(ctx, s, l.ID)
	default:
		r, err = fetchArtist(ctx, s, l.ID)
	}
	if err != nil {
		return nil, err // never a typed-nil pointer inside the interface
	}
	return r, nil
}
