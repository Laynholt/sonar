// Package model holds the service-neutral payload types shared by the
// Spotify and Amazon clients and the UI.
package model

// Service identifiers carried in every payload's Service field.
const (
	Spotify = "spotify"
	Amazon  = "amazon"
)

type TrackMetadata struct {
	Service     string `json:"service"`
	ID          string `json:"id,omitempty"`
	Artists     string `json:"artists"`
	Name        string `json:"name"`
	AlbumName   string `json:"album_name"`
	AlbumArtist string `json:"album_artist,omitempty"`
	DurationMS  int    `json:"duration_ms"`
	Images      string `json:"images"`
	ReleaseDate string `json:"release_date"`
	TrackNumber int    `json:"track_number"`
	TotalTracks int    `json:"total_tracks,omitempty"`
	DiscNumber  int    `json:"disc_number,omitempty"`
	TotalDiscs  int    `json:"total_discs,omitempty"`
	ExternalURL string `json:"external_urls"`
	Copyright   string `json:"copyright,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	Plays       string `json:"plays,omitempty"`
	PreviewURL  string `json:"preview_url,omitempty"`
	IsExplicit  bool   `json:"is_explicit,omitempty"`

	AlbumID     string         `json:"album_id,omitempty"`
	AlbumURL    string         `json:"album_url,omitempty"`
	ArtistID    string         `json:"artist_id,omitempty"`
	ArtistURL   string         `json:"artist_url,omitempty"`
	ArtistsData []ArtistSimple `json:"artists_data,omitempty"`
}

type ArtistSimple struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ExternalURL string `json:"external_urls"`
}

type AlbumTrackMetadata struct {
	Service     string         `json:"service"`
	ID          string         `json:"id,omitempty"`
	Artists     string         `json:"artists"`
	Name        string         `json:"name"`
	AlbumName   string         `json:"album_name"`
	AlbumArtist string         `json:"album_artist,omitempty"`
	DurationMS  int            `json:"duration_ms"`
	Images      string         `json:"images"`
	ReleaseDate string         `json:"release_date"`
	TrackNumber int            `json:"track_number"`
	TotalTracks int            `json:"total_tracks,omitempty"`
	DiscNumber  int            `json:"disc_number,omitempty"`
	TotalDiscs  int            `json:"total_discs,omitempty"`
	ExternalURL string         `json:"external_urls"`
	AlbumType   string         `json:"album_type,omitempty"`
	AlbumID     string         `json:"album_id,omitempty"`
	AlbumURL    string         `json:"album_url,omitempty"`
	ArtistID    string         `json:"artist_id,omitempty"`
	ArtistURL   string         `json:"artist_url,omitempty"`
	ArtistsData []ArtistSimple `json:"artists_data,omitempty"`
	Plays       string         `json:"plays,omitempty"`
	Status      string         `json:"status,omitempty"`
	PreviewURL  string         `json:"preview_url,omitempty"`
	IsExplicit  bool           `json:"is_explicit,omitempty"`
}

type TrackResponse struct {
	Service string        `json:"service"`
	Track   TrackMetadata `json:"track"`
}

type AlbumInfoMetadata struct {
	TotalTracks int    `json:"total_tracks"`
	Name        string `json:"name"`
	ReleaseDate string `json:"release_date"`
	Artists     string `json:"artists"`
	Images      string `json:"images"`
	Batch       string `json:"batch,omitempty"`
	ArtistID    string `json:"artist_id,omitempty"`
	ArtistURL   string `json:"artist_url,omitempty"`
	ExternalURL string `json:"external_urls"`
}

type AlbumResponsePayload struct {
	Service   string               `json:"service"`
	AlbumInfo AlbumInfoMetadata    `json:"album_info"`
	TrackList []AlbumTrackMetadata `json:"track_list"`
}

type PlaylistInfoMetadata struct {
	Tracks struct {
		Total int `json:"total"`
	} `json:"tracks"`
	Followers struct {
		Total int `json:"total"`
	} `json:"followers"`
	Owner struct {
		DisplayName string `json:"display_name"`
		Name        string `json:"name"`
		Images      string `json:"images"`
	} `json:"owner"`
	Cover       string `json:"cover,omitempty"`
	Description string `json:"description,omitempty"`
	Batch       string `json:"batch,omitempty"`
	ExternalURL string `json:"external_urls"`
}

type PlaylistResponsePayload struct {
	Service      string               `json:"service"`
	PlaylistInfo PlaylistInfoMetadata `json:"playlist_info"`
	TrackList    []AlbumTrackMetadata `json:"track_list"`
}

type ArtistInfoMetadata struct {
	Name            string   `json:"name"`
	Followers       int      `json:"followers"`
	Images          string   `json:"images"`
	Header          string   `json:"header,omitempty"`
	Gallery         []string `json:"gallery,omitempty"`
	ExternalURL     string   `json:"external_urls"`
	DiscographyType string   `json:"discography_type"`
	TotalAlbums     int      `json:"total_albums"`
	Biography       string   `json:"biography,omitempty"`
	Verified        bool     `json:"verified,omitempty"`
	Listeners       int      `json:"listeners,omitempty"`
	Rank            int      `json:"rank,omitempty"`
	Batch           string   `json:"batch,omitempty"`
}

type DiscographyAlbumMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AlbumType   string `json:"album_type"`
	ReleaseDate string `json:"release_date"`
	TotalTracks int    `json:"total_tracks"`
	Artists     string `json:"artists"`
	Images      string `json:"images"`
	ExternalURL string `json:"external_urls"`
}

type ArtistDiscographyPayload struct {
	Service    string                     `json:"service"`
	ArtistInfo ArtistInfoMetadata         `json:"artist_info"`
	AlbumList  []DiscographyAlbumMetadata `json:"album_list"`
	TrackList  []AlbumTrackMetadata       `json:"track_list"`
	TopTracks  []AlbumTrackMetadata       `json:"top_tracks"`
}

type SearchResult struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Artists     string `json:"artists,omitempty"`
	AlbumName   string `json:"album_name,omitempty"`
	Images      string `json:"images"`
	ReleaseDate string `json:"release_date,omitempty"`
	ExternalURL string `json:"external_urls"`
	Duration    int    `json:"duration_ms,omitempty"`
	TotalTracks int    `json:"total_tracks,omitempty"`
	Owner       string `json:"owner,omitempty"`
	IsExplicit  bool   `json:"is_explicit,omitempty"`
}

type SearchResponse struct {
	Tracks    []SearchResult `json:"tracks"`
	Albums    []SearchResult `json:"albums"`
	Artists   []SearchResult `json:"artists"`
	Playlists []SearchResult `json:"playlists"`
}

// TrackInfo is what the matcher needs about a source's track.
type TrackInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumID     string `json:"album_id"`
	Image       string `json:"image"`
	ReleaseDate string `json:"release_date"`
	ISRC        string `json:"isrc"`
	DurationMS  int    `json:"duration_ms"`
	TrackNumber int    `json:"track_number"`
	DiscNumber  int    `json:"disc_number"` // Amazon leaves it 0: its GraphQL has no such field (probed 2026-10-22)
	Explicit    bool   `json:"explicit"`
}

type AlbumInfo struct {
	ID          string      `json:"id"`
	Title       string      `json:"title"`
	Artist      string      `json:"artist"`
	Image       string      `json:"image"`
	ReleaseDate string      `json:"release_date"`
	Explicit    bool        `json:"explicit"`
	Tracks      []TrackInfo `json:"tracks"`
}
