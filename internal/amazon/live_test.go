package amazon

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"sonar/internal/model"
)

func liveCtx(t *testing.T) context.Context {
	t.Helper()
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1 to hit Amazon")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestLiveGetMetadata(t *testing.T) {
	ctx := liveCtx(t)
	base := "https://music.amazon.com/"

	r, err := GetMetadata(ctx, base+"tracks/B00A3SGXAA")
	if err != nil {
		t.Fatal(err)
	}
	if tr := r.(*model.TrackResponse).Track; tr.Name == "" || tr.AlbumName == "" {
		t.Errorf("track: %+v", tr)
	}

	r, err = GetMetadata(ctx, base+"albums/B00A3SGWYC")
	if err != nil {
		t.Fatal(err)
	}
	if al := r.(*model.AlbumResponsePayload); al.AlbumInfo.Name == "" || len(al.TrackList) == 0 {
		t.Errorf("album: %+v, %d tracks", al.AlbumInfo, len(al.TrackList))
	}

	r, err = GetMetadata(ctx, base+"playlists/B0CF3S6S4V")
	if err != nil {
		t.Fatal(err)
	}
	if pl := r.(*model.PlaylistResponsePayload); pl.PlaylistInfo.Owner.Name == "" || len(pl.TrackList) == 0 {
		t.Errorf("playlist: %+v, %d tracks", pl.PlaylistInfo, len(pl.TrackList))
	}

	r, err = GetMetadata(ctx, base+"artists/B06WP75FR9")
	if err != nil {
		t.Fatal(err)
	}
	if ar := r.(*model.ArtistDiscographyPayload); ar.ArtistInfo.Name == "" || len(ar.AlbumList) == 0 || len(ar.TrackList) == 0 {
		t.Errorf("artist: %+v, %d albums, %d tracks", ar.ArtistInfo, len(ar.AlbumList), len(ar.TrackList))
	}
}

func TestLiveSearchCatalog(t *testing.T) {
	r, err := SearchCatalog(liveCtx(t), "olivia rodrigo")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tracks) == 0 || len(r.Albums) == 0 || len(r.Artists) == 0 || len(r.Playlists) == 0 {
		t.Errorf("tracks=%d albums=%d artists=%d playlists=%d; want all > 0",
			len(r.Tracks), len(r.Albums), len(r.Artists), len(r.Playlists))
	}
}

func TestLiveLookup(t *testing.T) {
	ctx := liveCtx(t)
	got, err := LookupTracks(ctx, []string{"B00A3SGXAA"})
	if err != nil || len(got) != 1 || got[0].ISRC != "USRC11201326" {
		t.Errorf("LookupTracks = %+v, %v", got, err)
	}
	u, err := PreviewURL(ctx, "B00A3SGXAA")
	if err != nil || !strings.HasPrefix(u, "https://music.amazon.com/getSampleTrack/") {
		t.Errorf("PreviewURL = %q, %v", u, err)
	}
	al, err := LookupAlbum(ctx, "B00A3SGWYC")
	if err != nil || len(al.Tracks) == 0 || al.Tracks[0].ISRC == "" {
		t.Errorf("LookupAlbum = %+v, %v", al, err)
	}
}
