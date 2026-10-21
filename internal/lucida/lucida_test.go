package lucida

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sonar/internal/source"
)

const trackURL = "https://music.amazon.com/tracks/B00A3SGXAA"

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/data_track.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func fixturePage(t *testing.T) map[string]any {
	t.Helper()
	var f struct {
		Nodes []struct {
			Data []any `json:"data"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(fixture(t), &f); err != nil {
		t.Fatal(err)
	}
	return unflatten(f.Nodes[1].Data).(map[string]any)
}

func TestUnflatten(t *testing.T) {
	page := fixturePage(t)
	if page["download"] != trackURL {
		t.Errorf("download %v", page["download"])
	}
	if page["tokenExpiry"] != 1792698905.0 {
		t.Errorf("tokenExpiry %v", page["tokenExpiry"])
	}
	if tok, _ := page["token"].(string); len(tok) != 48 {
		t.Errorf("token %q", tok)
	}
	if page["info"].(map[string]any)["type"] != "track" {
		t.Errorf("info %v", page["info"])
	}
}

func TestUnflattenSpecials(t *testing.T) {
	got := unflatten([]any{map[string]any{"a": 1.0, "b": -1.0, "c": 2.0}, "x", []any{"Date", "2020"}})
	want := map[string]any{"a": "x", "b": nil, "c": "2020"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func newClient(srv *httptest.Server) *Client {
	return &Client{HTTP: srv.Client(), Base: srv.URL, PollEvery: time.Millisecond}
}

func TestDownloadFlow(t *testing.T) {
	page := fixturePage(t)
	primary := page["token"].(string)
	for range 2 {
		b, err := base64.StdEncoding.DecodeString(primary)
		if err != nil {
			t.Fatal(err)
		}
		primary = string(b)
	}
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch r.URL.Path {
		case "/__data.json":
			if q.Get("url") != trackURL || q.Get("country") != "auto" {
				t.Errorf("page query %v", q)
			}
			w.Write(fixture(t))
		case "/api/load":
			switch q.Get("url") {
			case "/api/fetch/stream/v2":
				var body struct {
					Downscale string `json:"downscale"`
					Private   bool   `json:"private"`
					Upload    struct{ Enabled bool }
					Token     struct {
						Primary string
						Expiry  float64
					}
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Downscale != "original" || !body.Private || body.Upload.Enabled ||
					body.Token.Primary != primary || body.Token.Expiry != 1792698905 {
					t.Errorf("stream body %+v", body)
				}
				w.Write([]byte(`{"success":true,"handoff":"h1","name":"srv"}`))
			case "/api/fetch/request/h1":
				if q.Get("force") != "srv" {
					t.Errorf("poll query %v", q)
				}
				if polls++; polls == 1 {
					w.Write([]byte(`{"success":true,"status":"ripping","message":"Ripping {item}..."}`))
				} else {
					w.Write([]byte(`{"success":true,"status":"completed"}`))
				}
			case "/api/fetch/request/h1/download":
				if q.Get("force") != "srv" || q.Get("redirect") != "true" {
					t.Errorf("download query %v", q)
				}
				w.Header().Set("Content-Type", "audio/flac")
				w.Write([]byte("fLaC-test"))
			default:
				t.Errorf("unexpected url %q", q.Get("url"))
			}
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	var statuses []string
	dir := t.TempDir()
	got, err := newClient(srv).Download(context.Background(), trackURL, filepath.Join(dir, "a", "b", "song"), func(s string) { statuses = append(statuses, s) })
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "a", "b", "song.flac"); got != want {
		t.Fatalf("path %q", got)
	}
	if b, _ := os.ReadFile(got); string(b) != "fLaC-test" {
		t.Fatalf("content %q", b)
	}
	if !reflect.DeepEqual(statuses, []string{"Ripping track..."}) {
		t.Fatalf("statuses %v", statuses)
	}
}

func TestDownloadErrors(t *testing.T) {
	tests := []struct {
		name        string
		page        func(w http.ResponseWriter)
		stream      string
		poll        string
		want        string
		unavailable bool
	}{
		{name: "info error", page: func(w http.ResponseWriter) { w.Write(pageWithInfoError(t)) }, want: "Not available"},
		{name: "stream error", stream: `{"success":false,"error":"quota"}`, want: "quota"},
		{name: "poll error", poll: `{"success":true,"status":"error","message":"rip failed"}`, want: "rip failed"},
		{name: "page 503", page: func(w http.ResponseWriter) { w.WriteHeader(503) }, want: "503", unavailable: true},
		{name: "page html", page: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte("<html>Just a moment...</html>"))
		}, want: "not JSON", unavailable: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Query().Get("url") {
				case "/api/fetch/stream/v2":
					w.Write([]byte(orDefault(tc.stream, `{"success":true,"handoff":"h1","name":"srv"}`)))
				case "/api/fetch/request/h1":
					w.Write([]byte(orDefault(tc.poll, `{"success":true,"status":"completed"}`)))
				default:
					if tc.page != nil {
						tc.page(w)
					} else {
						w.Write(fixture(t))
					}
				}
			}))
			defer srv.Close()
			_, err := newClient(srv).Download(context.Background(), trackURL, filepath.Join(t.TempDir(), "x"), nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.HasPrefix(err.Error(), "lucida: ") {
				t.Fatalf("err %v, want %q", err, tc.want)
			}
			if errors.Is(err, source.ErrUnavailable) != tc.unavailable {
				t.Fatalf("ErrUnavailable = %v for %v", !tc.unavailable, err)
			}
		})
	}
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// pageWithInfoError is a minimal devalue page whose info says success:false.
func pageWithInfoError(t *testing.T) []byte {
	t.Helper()
	data := []any{
		map[string]any{"info": 1.0},
		map[string]any{"success": 2.0, "error": 3.0},
		false,
		"Not available",
	}
	b, err := json.Marshal(map[string]any{"type": "data", "nodes": []any{map[string]any{"type": "data", "data": data}}})
	if err != nil {
		t.Fatal(err)
	}
	return b
}
