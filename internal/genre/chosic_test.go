package genre

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func fakeChosic(t *testing.T, handshake, track, artist string, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != 0 && r.Method == http.MethodGet {
			http.Error(w, "boom", status)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/tools/handshake/":
			http.SetCookie(w, &http.Cookie{Name: "sess", Value: "abc", Path: "/"})
			w.Write([]byte(handshake))
			return
		}
		if c, err := r.Cookie("sess"); err != nil || c.Value != "abc" {
			t.Errorf("%s: cookie missing", r.URL.Path)
		}
		if r.Header.Get("Origin") != "https://www.chosic.com" || r.Header.Get("Referer") != "https://www.chosic.com/music-genre-finder/" {
			t.Errorf("%s: bad Origin/Referer %q %q", r.URL.Path, r.Header.Get("Origin"), r.Header.Get("Referer"))
		}
		switch r.URL.Path {
		case "/api/tools/tracks/t1":
			w.Write([]byte(track))
		case "/api/tools/artists/a1":
			w.Write([]byte(artist))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestGenresFlow(t *testing.T) {
	srv := fakeChosic(t, `{"success":true}`, `{"artists":[{"id":"a1"},{"id":"a2"}]}`,
		`{"genres":["russian hip hop","Rap"," rap ","pop","trap","drill","grime"]}`, 0)
	defer srv.Close()
	got, err := (&Client{Base: srv.URL}).Genres(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"russian hip hop", "Rap", "pop", "trap", "drill"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestGenresErrors(t *testing.T) {
	okTrack, okArtist := `{"artists":[{"id":"a1"}]}`, `{"genres":["pop"]}`
	cases := map[string]*httptest.Server{
		"handshake false": fakeChosic(t, `{"success":false}`, okTrack, okArtist, 0),
		"no artists":      fakeChosic(t, `{"success":true}`, `{"artists":[]}`, okArtist, 0),
		"no genres":       fakeChosic(t, `{"success":true}`, okTrack, `{"genres":[]}`, 0),
		"http 500":        fakeChosic(t, `{"success":true}`, okTrack, okArtist, 500),
	}
	for name, srv := range cases {
		t.Run(name, func(t *testing.T) {
			defer srv.Close()
			if g, err := (&Client{Base: srv.URL}).Genres(context.Background(), "t1"); err == nil {
				t.Fatalf("want error, got %q", g)
			}
		})
	}
}

func TestLiveGenres(t *testing.T) {
	if os.Getenv("SONAR_LIVE") != "1" {
		t.Skip("set SONAR_LIVE=1")
	}
	g, err := (&Client{HTTP: liveClient()}).Genres(context.Background(), "0VjIjW4GlUZAMYd2vXMi3b")
	if err != nil || len(g) == 0 {
		t.Fatalf("genres %q err %v", g, err)
	}
	t.Logf("genres: %q", g)
}
