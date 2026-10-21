package amazon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeQuerier answers from handle(root, vars); order matters because the artist query also contains "albums(".
type fakeQuerier struct {
	handle func(root string, vars map[string]any) []byte
	calls  []map[string]any
	roots  []string
}

func (f *fakeQuerier) gql(_ context.Context, query string, vars map[string]any, out any) error {
	root := ""
	for _, r := range []string{"artist(", "albums(", "playlist(", "track(", "album("} {
		if strings.Contains(query, r) {
			root = r
			break
		}
	}
	f.roots = append(f.roots, root)
	f.calls = append(f.calls, vars)
	return decodeGQL(f.handle(root, vars), out)
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func noMorePages(b []byte) []byte {
	return bytes.ReplaceAll(b, []byte(`"hasNextPage":true`), []byte(`"hasNextPage":false`))
}

func fileQuerier(t *testing.T, root, name string) *fakeQuerier {
	return &fakeQuerier{handle: func(r string, _ map[string]any) []byte {
		if r != root {
			t.Fatalf("unexpected root %q", r)
		}
		return fixture(t, name)
	}}
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

const (
	cover = "https://m.media-amazon.com/images/I/81c5BZ704HL.jpg"
	pit   = "https://music.amazon.com/artists/B000QJNL2Y"
)

func TestFetchTrack(t *testing.T) {
	r, err := fetchTrack(context.Background(), fileQuerier(t, "track(", "gql_track.json"), "B00A3SGXAA")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "service", r.Service, "amazon")
	tr := r.Track
	eq(t, "Service", tr.Service, "amazon")
	eq(t, "ID", tr.ID, "B00A3SGXAA")
	eq(t, "Name", tr.Name, "Global Warming")
	eq(t, "Artists", tr.Artists, "Pitbull, Sensato")
	eq(t, "AlbumName", tr.AlbumName, "Global Warming")
	eq(t, "AlbumArtist", tr.AlbumArtist, "Pitbull")
	eq(t, "DurationMS", tr.DurationMS, 85000)
	eq(t, "Images", tr.Images, cover)
	eq(t, "ReleaseDate", tr.ReleaseDate, "2012-11-16")
	eq(t, "TrackNumber", tr.TrackNumber, 1)
	eq(t, "TotalTracks", tr.TotalTracks, 12)
	eq(t, "ExternalURL", tr.ExternalURL, "https://music.amazon.com/tracks/B00A3SGXAA")
	eq(t, "Copyright", tr.Copyright, "(P) 2012 RCA Records, a division of Sony Music Entertainment")
	eq(t, "IsExplicit", tr.IsExplicit, true)
	eq(t, "AlbumID", tr.AlbumID, "B00A3SGWYC")
	eq(t, "AlbumURL", tr.AlbumURL, "https://music.amazon.com/albums/B00A3SGWYC")
	eq(t, "ArtistID", tr.ArtistID, "B000QJNL2Y")
	eq(t, "ArtistURL", tr.ArtistURL, pit)
	eq(t, "len(ArtistsData)", len(tr.ArtistsData), 2)
	eq(t, "ArtistsData[1].Name", tr.ArtistsData[1].Name, "Sensato")
	eq(t, "ArtistsData[1].ExternalURL", tr.ArtistsData[1].ExternalURL, "https://music.amazon.com/artists/B003AC4R0Y")
}

func TestFetchAlbum(t *testing.T) {
	r, err := fetchAlbum(context.Background(), fileQuerier(t, "album(", "gql_album.json"), "B00A3SGWYC")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "Service", r.Service, "amazon")
	i := r.AlbumInfo
	eq(t, "Name", i.Name, "Global Warming")
	eq(t, "TotalTracks", i.TotalTracks, 12)
	eq(t, "ReleaseDate", i.ReleaseDate, "2012-11-16")
	eq(t, "Artists", i.Artists, "Pitbull")
	eq(t, "Images", i.Images, cover)
	eq(t, "ArtistID", i.ArtistID, "B000QJNL2Y")
	eq(t, "ExternalURL", i.ExternalURL, "https://music.amazon.com/albums/B00A3SGWYC")
	eq(t, "len(TrackList)", len(r.TrackList), 12)
	tr := r.TrackList[0]
	eq(t, "Service", tr.Service, "amazon")
	eq(t, "Name", tr.Name, "Global Warming")
	eq(t, "Artists", tr.Artists, "Pitbull, Sensato")
	eq(t, "TrackNumber", tr.TrackNumber, 1)
	eq(t, "DurationMS", tr.DurationMS, 85000)
	eq(t, "AlbumName", tr.AlbumName, "Global Warming")
	eq(t, "Images", tr.Images, cover)
	eq(t, "IsExplicit", tr.IsExplicit, true)
}

func TestFetchPlaylist(t *testing.T) {
	p1, p2 := fixture(t, "gql_playlist_p1.json"), noMorePages(fixture(t, "gql_playlist_p2.json"))
	var tok struct {
		Data struct {
			Playlist struct {
				Tracks struct {
					PageInfo struct{ Token string }
				}
			}
		}
	}
	if err := json.Unmarshal(p1, &tok); err != nil {
		t.Fatal(err)
	}
	token := tok.Data.Playlist.Tracks.PageInfo.Token
	q := &fakeQuerier{handle: func(_ string, vars map[string]any) []byte {
		switch vars["after"] {
		case nil:
			return p1
		case token:
			return p2
		}
		t.Fatalf("unexpected after %v", vars["after"])
		return nil
	}}
	r, err := fetchPlaylist(context.Background(), q, "B0CF3S6S4V")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "calls", len(q.calls), 2)
	eq(t, "Service", r.Service, "amazon")
	i := r.PlaylistInfo
	eq(t, "Owner.Name", i.Owner.Name, "REDISCOVER Olivia Rodrigo")
	eq(t, "Owner.DisplayName", i.Owner.DisplayName, "Amazon Music")
	eq(t, "Tracks.Total", i.Tracks.Total, 48)
	eq(t, "Followers.Total", i.Followers.Total, 0)
	eq(t, "Cover", i.Cover, "https://m.media-amazon.com/images/I/A1OglwNNGNL.jpg")
	eq(t, "Description", i.Description, "Your definitive collection of all things Olivia Rodrigo.")
	eq(t, "ExternalURL", i.ExternalURL, "https://music.amazon.com/playlists/B0CF3S6S4V")
	var ids []string
	for _, tr := range r.TrackList {
		ids = append(ids, tr.ID)
		eq(t, "track Service", tr.Service, "amazon")
	}
	want := []string{"B0GVMRQZFH", "B0HM6HQLRZ", "B091BJPW5T", "B091BJ9SJZ", "B091BLRP9Q", "B0HMY12BH5"}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	eq(t, "[0].Name", r.TrackList[0].Name, "drop dead")
	eq(t, "[0].IsExplicit", r.TrackList[0].IsExplicit, true)
	eq(t, "[0].Images", r.TrackList[0].Images, "https://m.media-amazon.com/images/I/61PIlNHqZ8L.jpg")
	eq(t, "[1].AlbumName", r.TrackList[1].AlbumName, "Olivia Rodrigo (Amazon Music Songline)")
	eq(t, "[1].DurationMS", r.TrackList[1].DurationMS, 211000)
}

func TestFetchArtist(t *testing.T) {
	q := &fakeQuerier{handle: func(root string, _ map[string]any) []byte {
		switch root {
		case "artist(":
			return noMorePages(fixture(t, "gql_artist.json"))
		case "albums(":
			return fixture(t, "gql_albums.json")
		}
		t.Fatalf("unexpected root %q", root)
		return nil
	}}
	r, err := fetchArtist(context.Background(), q, "B06WP75FR9")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "Service", r.Service, "amazon")
	i := r.ArtistInfo
	eq(t, "Name", i.Name, "Olivia Rodrigo")
	eq(t, "Images", i.Images, "https://m.media-amazon.com/images/I/717Oy0K6lvL.jpg")
	eq(t, "Header", i.Header, "https://m.media-amazon.com/images/I/61PIlMg5XTL.jpg")
	eq(t, "ExternalURL", i.ExternalURL, "https://music.amazon.com/artists/B06WP75FR9")
	eq(t, "DiscographyType", i.DiscographyType, "all")
	eq(t, "TotalAlbums", i.TotalAlbums, 3)
	a := r.AlbumList[0]
	eq(t, "ID", a.ID, "B0GVN6XSJC")
	eq(t, "Name", a.Name, "you seem pretty sad for a girl so in love")
	eq(t, "AlbumType", a.AlbumType, "album")
	eq(t, "ReleaseDate", a.ReleaseDate, "2026-06-11")
	eq(t, "TotalTracks", a.TotalTracks, 13)
	eq(t, "Artists", a.Artists, "Olivia Rodrigo")
	eq(t, "ExternalURL", a.ExternalURL, "https://music.amazon.com/albums/B0GVN6XSJC")
	eq(t, "[2].AlbumType", r.AlbumList[2].AlbumType, "single")
	eq(t, "len(TrackList)", len(r.TrackList), 26)
	// album order: 13 tracks of album 0 first, and every track points at its album
	n := 0
	for _, al := range r.AlbumList {
		for ; n < len(r.TrackList) && r.TrackList[n].AlbumID == al.ID; n++ {
		}
	}
	eq(t, "tracks grouped in album order", n, 26)
}

func TestFetchArtistPagesAndBatches(t *testing.T) {
	const total = 150
	node := func(i int) map[string]any {
		return map[string]any{"id": fmt.Sprintf("A%03d", i), "title": "t", "format": "ALBUM", "trackCount": 1}
	}
	page := func(from, to int, next bool, token string) []byte {
		var edges []map[string]any
		for i := from; i < to; i++ {
			edges = append(edges, map[string]any{"node": node(i)})
		}
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"artist": map[string]any{
			"id": "X", "name": "X", "images": []any{},
			"albums": map[string]any{"pageInfo": map[string]any{"hasNextPage": next, "token": token}, "edges": edges},
		}}})
		return b
	}
	var batches []int
	q := &fakeQuerier{}
	q.handle = func(root string, vars map[string]any) []byte {
		if root == "artist(" {
			if vars["cursor"] == nil {
				return page(0, 100, true, "tok1")
			}
			return page(100, total, false, "")
		}
		ids := vars["ids"].([]string)
		batches = append(batches, len(ids))
		var albums []map[string]any
		for _, id := range ids {
			albums = append(albums, map[string]any{"id": id, "title": "t", "trackCount": 1,
				"tracks": []map[string]any{{"id": "T" + id, "title": "s", "duration": 1, "trackNumber": 1}}})
		}
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"albums": albums}})
		return b
	}
	r, err := fetchArtist(context.Background(), q, "X")
	if err != nil {
		t.Fatal(err)
	}
	var artistCalls []map[string]any
	for i, root := range q.roots {
		if root == "artist(" {
			artistCalls = append(artistCalls, q.calls[i])
		}
	}
	eq(t, "artist calls", len(artistCalls), 2)
	eq(t, "second cursor", fmt.Sprint(artistCalls[1]["cursor"]), "tok1")
	if !slices.Equal(batches, []int{100, 50}) {
		t.Errorf("batches = %v, want [100 50]", batches)
	}
	eq(t, "len(AlbumList)", len(r.AlbumList), total)
	eq(t, "len(TrackList)", len(r.TrackList), total)
}

func TestFetchNotFound(t *testing.T) {
	q := &fakeQuerier{handle: func(string, map[string]any) []byte { return fixture(t, "gql_err_notfound.json") }}
	ctx := context.Background()
	fetchers := map[string]func() error{
		"track":    func() error { _, err := fetchTrack(ctx, q, "B000000000"); return err },
		"album":    func() error { _, err := fetchAlbum(ctx, q, "B000000000"); return err },
		"playlist": func() error { _, err := fetchPlaylist(ctx, q, "B000000000"); return err },
		"artist":   func() error { _, err := fetchArtist(ctx, q, "B000000000"); return err },
	}
	for name, f := range fetchers {
		err := f()
		if err == nil || !strings.Contains(err.Error(), "not found in the US catalog") || !strings.Contains(err.Error(), "B000000000") {
			t.Errorf("%s: err = %v, want 'not found in the US catalog' with the ID", name, err)
		}
	}
}

func TestFetchArtistSurvivesFailedBatch(t *testing.T) {
	var edges []map[string]any
	for i := range 150 {
		edges = append(edges, map[string]any{"node": map[string]any{"id": fmt.Sprintf("A%03d", i), "title": "t", "format": "ALBUM", "trackCount": 1}})
	}
	artist, _ := json.Marshal(map[string]any{"data": map[string]any{"artist": map[string]any{
		"id": "X", "name": "X", "images": []any{},
		"albums": map[string]any{"pageInfo": map[string]any{"hasNextPage": false}, "edges": edges},
	}}})
	q := &fakeQuerier{}
	q.handle = func(root string, vars map[string]any) []byte {
		if root == "artist(" {
			return artist
		}
		ids := vars["ids"].([]string)
		if ids[0] == "A000" {
			return []byte(`{"errors":[{"message":"boom"}]}`)
		}
		var albums []map[string]any
		for _, id := range ids {
			albums = append(albums, map[string]any{"id": id, "title": "t", "trackCount": 1,
				"tracks": []map[string]any{{"id": "T" + id, "title": "s", "duration": 1, "trackNumber": 1}}})
		}
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"albums": albums}})
		return b
	}
	r, err := fetchArtist(context.Background(), q, "X")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "len(AlbumList)", len(r.AlbumList), 150)
	eq(t, "len(TrackList)", len(r.TrackList), 50)
}
