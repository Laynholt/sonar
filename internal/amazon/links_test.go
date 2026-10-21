package amazon

import (
	"errors"
	"testing"
)

func TestParseLink(t *testing.T) {
	tests := []struct {
		in      string
		want    Link
		wantErr error
		anyErr  bool
	}{
		{in: "https://music.amazon.com/albums/B00A3SGWYC", want: Link{"album", "B00A3SGWYC"}},
		{in: "https://music.amazon.com/albums/B00A3SGWYC?trackAsin=B00A3SGXAA&ref=x", want: Link{"track", "B00A3SGXAA"}},
		{in: "https://music.amazon.com/tracks/B00A3SGXAA/?ref=dm_ff", want: Link{"track", "B00A3SGXAA"}},
		{in: "https://music.amazon.com/playlists/B0CF3S6S4V", want: Link{"playlist", "B0CF3S6S4V"}},
		{in: "https://music.amazon.com/my/playlists/B0CF3S6S4V?ref=x", want: Link{"playlist", "B0CF3S6S4V"}},
		{in: "https://music.amazon.co.uk/artists/B06WP75FR9/olivia-rodrigo", want: Link{"artist", "B06WP75FR9"}},
		{in: "https://www.music.amazon.com/albums/B00A3SGWYC", want: Link{"album", "B00A3SGWYC"}},
		{in: "music.amazon.com/albums/B00A3SGWYC", want: Link{"album", "B00A3SGWYC"}},
		{in: "https://music.amazon.com/user-playlists/f02fa03d3e404c428e53d4cfb58ce93bc2a0", wantErr: ErrCommunityPlaylist},
		{in: "https://music.amazon.com/search/olivia", anyErr: true},
		{in: "https://music.amazon.com/albums/notanasin", anyErr: true},
		{in: "https://music.amazon.com/my", anyErr: true},
	}
	for _, tc := range tests {
		got, err := ParseLink(tc.in)
		switch {
		case tc.wantErr != nil:
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("%s: err = %v, want %v", tc.in, err, tc.wantErr)
			}
		case tc.anyErr:
			if err == nil || errors.Is(err, ErrCommunityPlaylist) {
				t.Errorf("%s: err = %v, want a non-community error", tc.in, err)
			}
		case err != nil || got != tc.want:
			t.Errorf("%s: got %+v, %v; want %+v", tc.in, got, err, tc.want)
		}
	}
}

func TestIsLink(t *testing.T) {
	for in, want := range map[string]bool{
		"https://music.amazon.com/albums/B00A3SGWYC":                   true,
		"https://music.amazon.co.uk/artists/B06WP75FR9/olivia-rodrigo": true,
		"music.amazon.com/albums/B00A3SGWYC":                           true,
		"https://www.amazon.com/dp/B00A3SGWYC":                         false,
		"https://open.spotify.com/track/x":                             false,
	} {
		if got := IsLink(in); got != want {
			t.Errorf("IsLink(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestTrackURL(t *testing.T) {
	if u, err := TrackURL("B00A3SGXAA"); u != "https://music.amazon.com/tracks/B00A3SGXAA" || err != nil {
		t.Errorf("got %q, %v", u, err)
	}
	for _, id := range []string{"6rqhFgbbKwnb9MLmUQDhG6", "../x", ""} {
		if u, err := TrackURL(id); err == nil {
			t.Errorf("TrackURL(%q) = %q, want error", id, u)
		}
	}
}
