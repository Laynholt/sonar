package amazon

import (
	"context"
	"encoding/json"
	"testing"
)

// trackQuerier answers track(id) from the recorded map; other ids are null.
func trackQuerier(t *testing.T) *fakeQuerier {
	var rec map[string]json.RawMessage
	if err := json.Unmarshal(fixture(t, "gql_lookup_track.json"), &rec); err != nil {
		t.Fatal(err)
	}
	return &fakeQuerier{handle: func(_ string, vars map[string]any) []byte {
		if b, ok := rec[vars["id"].(string)]; ok {
			return b
		}
		return []byte(`{"data":{"track":null}}`)
	}}
}

func TestLookupTracksKeepsOrder(t *testing.T) {
	got, err := lookupTracks(context.Background(), trackQuerier(t), []string{"B00A3SGXTQ", "B00A3SGXAA"})
	if err != nil || len(got) != 2 {
		t.Fatalf("got %+v, %v", got, err)
	}
	b, a := got[0], got[1]
	eq(t, "b.ID", b.ID, "B00A3SGXTQ")
	eq(t, "b.Title", b.Title, "Don't Stop the Party")
	eq(t, "b.ISRC", b.ISRC, "USRC11201120")
	eq(t, "b.DurationMS", b.DurationMS, 206000)
	eq(t, "a.ID", a.ID, "B00A3SGXAA")
	eq(t, "a.Title", a.Title, "Global Warming")
	eq(t, "a.Artist", a.Artist, "Pitbull, Sensato")
	eq(t, "a.ISRC", a.ISRC, "USRC11201326")
	eq(t, "a.DurationMS", a.DurationMS, 85000)
	eq(t, "a.Album", a.Album, "Global Warming")
	eq(t, "a.AlbumID", a.AlbumID, "B00A3SGWYC")
	eq(t, "a.Image", a.Image, cover)
	eq(t, "a.ReleaseDate", a.ReleaseDate, "2012-11-16")
	eq(t, "a.TrackNumber", a.TrackNumber, 1)
	eq(t, "a.Explicit", a.Explicit, true)
}

func TestLookupTracksDropsUnknown(t *testing.T) {
	got, err := lookupTracks(context.Background(), trackQuerier(t), []string{"NOSUCHID00", "B00A3SGXAA"})
	if err != nil || len(got) != 1 || got[0].ID != "B00A3SGXAA" {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestLookupAlbumTracks(t *testing.T) {
	al, err := lookupAlbum(context.Background(), fileQuerier(t, "album(", "gql_lookup_album.json"), "B00A3SGWYC")
	if err != nil {
		t.Fatal(err)
	}
	eq(t, "Title", al.Title, "Global Warming")
	eq(t, "Artist", al.Artist, "Pitbull")
	eq(t, "Image", al.Image, cover)
	eq(t, "ReleaseDate", al.ReleaseDate, "2012-11-16")
	eq(t, "Explicit", al.Explicit, true)
	if len(al.Tracks) != 3 {
		t.Fatalf("tracks = %d", len(al.Tracks))
	}
	for i, want := range []struct{ id, isrc string }{{"B00A3SGXAA", "USRC11201326"}, {"B00A3SGXTQ", "USRC11201120"}, {"", "USRC11201328"}} {
		tr := al.Tracks[i]
		if tr.ID == "" || want.id != "" && tr.ID != want.id {
			t.Errorf("track %d ID = %q", i, tr.ID)
		}
		eq(t, "ISRC", tr.ISRC, want.isrc)
		eq(t, "TrackNumber", tr.TrackNumber, i+1)
		eq(t, "Album", tr.Album, "Global Warming")
		eq(t, "AlbumID", tr.AlbumID, "B00A3SGWYC")
	}
}

func TestPreviewURL(t *testing.T) {
	q := trackQuerier(t)
	got, err := previewURL(context.Background(), q, "B00A3SGXAA")
	eq(t, "url", got, "https://music.amazon.com/getSampleTrack/B00A3SGXAA?ref=dm_ff_amazonmusic_dragonfly_web_anonymous")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := previewURL(context.Background(), q, "NOSUCHID00"); got != "" || err != nil {
		t.Errorf("unknown track: %q, %v", got, err)
	}
}
