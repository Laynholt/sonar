package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"sonar/internal/model"
)

func TestIsLink(t *testing.T) {
	ok := []string{
		"https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8",
		"  https://open.spotify.com/intl-de/album/6N9PS4QXF1D0OWPk0Sxtb4?si=abc  ",
		"spotify:album:6N9PS4QXF1D0OWPk0Sxtb4",
		"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M",
		"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt/discography/all",
	}
	for _, u := range ok {
		if !IsLink(u) {
			t.Errorf("want link: %q", u)
		}
	}
	for _, u := range []string{"", "https://music.amazon.com/albums/B00A3SGWYC", "https://open.spotify.com/track/short"} {
		if IsLink(u) {
			t.Errorf("not a link: %q", u)
		}
	}
}

func TestCheckStatusRegionBlocked(t *testing.T) {
	err := checkStatus(&http.Response{StatusCode: 451, Body: io.NopCloser(strings.NewReader(""))})
	if !errors.Is(err, ErrRegionBlocked) {
		t.Fatalf("got %v", err)
	}
	if checkStatus(&http.Response{StatusCode: 200, Body: http.NoBody}) != nil {
		t.Fatal("200 must pass")
	}
	if err := checkStatus(&http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("boom"))}); err == nil || errors.Is(err, ErrRegionBlocked) {
		t.Fatalf("500 must be a plain error, got %v", err)
	}
}

func TestFormatAlbumSetsServiceAndIDs(t *testing.T) {
	var album apiAlbumResponse
	const raw = `{"id":"6N9PS4QXF1D0OWPk0Sxtb4","name":"A","artists":"X","count":2,"discs":{"totalCount":2},
		"tracks":[{"id":"1111111111111111111111","disc_number":1,"duration":"3:00"},
		          {"id":"2222222222222222222222","disc_number":2,"duration":"4:00"}]}`
	if err := json.Unmarshal([]byte(raw), &album); err != nil {
		t.Fatal(err)
	}
	got, err := (&metadataClient{}).formatAlbumData(&album)
	if err != nil {
		t.Fatal(err)
	}
	if got.Service != model.Spotify {
		t.Errorf("payload Service = %q", got.Service)
	}
	if len(got.TrackList) != 2 {
		t.Fatalf("tracks = %d", len(got.TrackList))
	}
	for _, tr := range got.TrackList {
		if tr.Service != model.Spotify || tr.ID == "" || tr.ExternalURL != "https://open.spotify.com/track/"+tr.ID {
			t.Errorf("bad track: %+v", tr)
		}
	}
	if got.TrackList[1].DiscNumber != 2 {
		t.Errorf("disc = %d", got.TrackList[1].DiscNumber)
	}
}

func TestLive(t *testing.T) {
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cases := []struct {
		url, kind string
	}{
		{"https://open.spotify.com/track/4PTG3Z6ehGkBFwjybzWkR8", "track"},
		{"https://open.spotify.com/intl-de/album/6N9PS4QXF1D0OWPk0Sxtb4", "album"},
		{"https://open.spotify.com/playlist/37i9dQZF1DXcBWIGoYBM5M", "playlist"},
		{"https://open.spotify.com/artist/0gxyHStUsqpMadRV0Di1Qt/discography/all", "artist"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			got, err := GetMetadata(ctx, c.url)
			if err != nil {
				t.Fatal(err)
			}
			var name string
			var tracks []model.AlbumTrackMetadata
			var service string
			switch p := got.(type) {
			case *model.TrackResponse:
				if c.kind != "track" {
					t.Fatalf("got %T", got)
				}
				name, service = p.Track.Name, p.Service
				if p.Track.Service != model.Spotify {
					t.Errorf("track Service = %q", p.Track.Service)
				}
			case *model.AlbumResponsePayload:
				if c.kind != "album" {
					t.Fatalf("got %T", got)
				}
				name, tracks, service = p.AlbumInfo.Name, p.TrackList, p.Service
			case *model.PlaylistResponsePayload:
				if c.kind != "playlist" {
					t.Fatalf("got %T", got)
				}
				name, tracks, service = p.PlaylistInfo.Owner.Name, p.TrackList, p.Service
			case *model.ArtistDiscographyPayload:
				if c.kind != "artist" {
					t.Fatalf("got %T", got)
				}
				name, tracks, service = p.ArtistInfo.Name, p.TrackList, p.Service
				if len(p.AlbumList) == 0 {
					t.Error("empty AlbumList")
				}
			default:
				t.Fatalf("unexpected %T", got)
			}
			if name == "" {
				t.Error("empty name")
			}
			if service != model.Spotify {
				t.Errorf("payload Service = %q", service)
			}
			if c.kind != "track" && len(tracks) == 0 {
				t.Error("empty TrackList")
			}
			for _, tr := range tracks {
				if tr.Service != model.Spotify || tr.ID == "" {
					t.Fatalf("bad track %+v", tr)
				}
			}
		})
	}

	res, err := Search(ctx, "never gonna give you up", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Tracks) == 0 {
		t.Error("search returned no tracks")
	}
}
