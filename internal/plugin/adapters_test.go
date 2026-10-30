package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"sonar/internal/download"
	"sonar/internal/match"
	"sonar/internal/model"
)

const allCan = `["metadata","search","searchByType","find","tracks","album","isrc","preview","parseLink"]`

// putSource adds the fake to the built-in folder as a source; links and can are JSON arrays.
func putSource(t *testing.T, e *regEnv, id, service, links, can string) {
	t.Helper()
	e.bless(t, id, putFakeManifest(t, e.builtin, id, `{"id":"`+id+`","name":"`+id+`","version":"1.0.0","kind":"source","run":["fake"],`+
		`"source":{"service":"`+service+`","links":`+links+`,"can":`+can+`}}`))
}

func putTagger(t *testing.T, e *regEnv, id string) {
	t.Helper()
	e.bless(t, id, putFakeManifest(t, e.builtin, id, `{"id":"`+id+`","name":"`+id+`","version":"1.0.0","kind":"tagger","run":["fake"],`+
		`"tagger":{"fields":["genre"],"needs":"amazon"}}`))
}

func TestSourceClientCalls(t *testing.T) {
	e := newEnv(t)
	putSource(t, e, "src", "svc", `[]`, `["metadata","search","searchByType","find","tracks","album","isrc","parseLink"]`) // no preview
	r := e.open(t)
	ctx := context.Background()

	s, err := r.Source("svc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Preview(ctx, "x"); !errors.Is(err, ErrCannot) {
		t.Fatalf("Preview without the capability: %v", err)
	}
	if len(r.hosts) != 0 {
		t.Fatal("ErrCannot started a host")
	}

	raw, err := s.Metadata(ctx, "http://u")
	if err != nil || string(raw) != `{"url":"http://u"}` {
		t.Fatalf("Metadata: %s, %v", raw, err)
	}
	sr, err := s.Search(ctx, "q", 7)
	if err != nil || !reflect.DeepEqual(sr.Tracks, []model.SearchResult{{ID: "t1", Name: "q", Type: "track", Duration: 7}}) {
		t.Fatalf("Search: %+v, %v", sr, err)
	}
	bt, err := s.SearchByType(ctx, "q", "album", 5, 10)
	if err != nil || !reflect.DeepEqual(bt, []model.SearchResult{{ID: "10", Name: "q", Type: "album", Duration: 5}}) {
		t.Fatalf("SearchByType: %+v, %v", bt, err)
	}
	f, err := s.Find(ctx, "q")
	if err != nil || !reflect.DeepEqual(f, []match.Item{{Kind: "track", ID: "m1", Title: "q", Score: 0.9}}) {
		t.Fatalf("Find: %+v, %v", f, err)
	}
	tr, err := s.Tracks(ctx, []string{"a", "b"})
	if err != nil || len(tr) != 2 || tr[1].ISRC != "US-b" || tr[1].DurationMS != 1000 {
		t.Fatalf("Tracks: %+v, %v", tr, err)
	}
	al, err := s.Album(ctx, "alb")
	if err != nil || al.ID != "alb" || len(al.Tracks) != 1 || al.Tracks[0].ISRC != "US-1" {
		t.Fatalf("Album: %+v, %v", al, err)
	}
	ids, err := s.ISRC(ctx, "I")
	if err != nil || !reflect.DeepEqual(ids, []string{"I-1", "I-2"}) {
		t.Fatalf("ISRC: %v, %v", ids, err)
	}
	l, err := s.ParseLink(ctx, "http://u")
	if err != nil || l != (Link{Kind: "track", ID: "http://u"}) {
		t.Fatalf("ParseLink: %+v, %v", l, err)
	}
}

func TestSourcePreview(t *testing.T) {
	e := newEnv(t)
	putSource(t, e, "src", "svc", `[]`, allCan)
	s, err := e.open(t).Source("svc")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.Preview(context.Background(), "x"); err != nil || got != "http://preview/x" {
		t.Fatalf("Preview: %q, %v", got, err)
	}
}

func TestSourceForLinkOrder(t *testing.T) {
	e := newEnv(t)
	putSource(t, e, "bad", "s0", `["("]`, allCan) // does not compile: never matches, logged once
	putSource(t, e, "one", "s1", `["http://site/.*"]`, allCan)
	putSource(t, e, "two", "s2", `["http://site/x"]`, allCan)
	r := e.open(t)
	if err := r.SetOrder([]string{"bad", "two", "one"}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		s, ok := r.SourceForLink("http://site/x")
		if !ok || s.ID != "two" || s.Service != "s2" {
			t.Fatalf("got %+v, %v", s, ok)
		}
	}
	if _, ok := r.SourceForLink("http://other/x"); ok {
		t.Fatal("matched a foreign URL")
	}
	if n := e.l.count(`source.links "("`); n != 1 {
		t.Fatalf("bad pattern logged %d times", n)
	}
	// A disabled source is skipped even when it matches first.
	if err := r.SetEnabled("two", false, false); err != nil {
		t.Fatal(err)
	}
	if s, ok := r.SourceForLink("http://site/x"); !ok || s.ID != "one" {
		t.Fatalf("after disabling: %+v, %v", s, ok)
	}
}

func TestSourceByServiceOrder(t *testing.T) {
	e := newEnv(t)
	putSource(t, e, "a", "same", `[]`, allCan)
	putSource(t, e, "b", "same", `[]`, allCan)
	r := e.open(t)
	if err := r.SetOrder([]string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	if s, err := r.Source("same"); err != nil || s.ID != "b" {
		t.Fatalf("got %+v, %v", s, err)
	}
}

func TestSourceDisabled(t *testing.T) {
	e := newEnv(t)
	putSource(t, e, "src", "x", `[]`, allCan)
	r := e.open(t)
	if err := r.SetEnabled("src", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Source("x"); err == nil || !strings.Contains(err.Error(), `"x"`) {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.Source("nope"); err == nil {
		t.Fatal("unknown service")
	}
}

func TestTaggersOrder(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"t1", "t2", "t3"} {
		putTagger(t, e, id)
	}
	r := e.open(t)
	if err := r.SetOrder([]string{"t3", "t1", "t2"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled("t1", false, false); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, tc := range r.Taggers() {
		ids = append(ids, tc.ID)
	}
	if !reflect.DeepEqual(ids, []string{"t3", "t2"}) {
		t.Fatalf("got %v", ids)
	}
	tc := r.Taggers()[0]
	if !reflect.DeepEqual(tc.Fields, []string{"genre"}) || tc.Needs != "amazon" {
		t.Fatalf("%+v", tc)
	}
	got, err := tc.Tag(context.Background(), TagParams{Track: download.Track{Title: "S", ISRC: "I"}, IDs: map[string]string{"x": "7"}})
	if err != nil || !reflect.DeepEqual(got, map[string]string{"title": "S", "isrc": "I", "id": "7"}) {
		t.Fatalf("Tag: %v, %v", got, err)
	}
}

func TestModelInfoJSON(t *testing.T) {
	in := model.TrackInfo{ID: "1", ISRC: "US1", DurationMS: 1234, TrackNumber: 2, Explicit: true}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"isrc":"US1"`, `"duration_ms":1234`, `"track_number":2`, `"album_id"`} {
		if !strings.Contains(string(raw), k) {
			t.Errorf("%s lacks %s", raw, k)
		}
	}
	var out model.TrackInfo
	if err := json.Unmarshal(raw, &out); err != nil || out != in {
		t.Fatalf("round trip: %+v, %v", out, err)
	}
}

func TestSourceForLinkWholeURL(t *testing.T) {
	e := newEnv(t)
	putSource(t, e, "src", "svc", `["https://open[.]spotify[.]com/track/[a-z]+"]`, allCan)
	r := e.open(t)
	if _, ok := r.SourceForLink("https://open.spotify.com/track/abc"); !ok {
		t.Fatal("no match for the real URL")
	}
	for _, u := range []string{
		"https://evil.example/?u=https://open.spotify.com/track/1",
		"https://open.spotify.com/track/abc/extra",
		"x https://open.spotify.com/track/abc",
	} {
		if _, ok := r.SourceForLink(u); ok {
			t.Errorf("matched %q", u)
		}
	}
}

// A user-folder source replaced on disk with other links must follow the new patterns after Refresh.
func TestSourceForLinkReplacedCopy(t *testing.T) {
	e := newEnv(t)
	put := func(version, link string) {
		putFakeManifest(t, e.user, "usr", `{"id":"usr","name":"usr","version":"`+version+`","kind":"source","run":["fake"],`+
			`"source":{"service":"svc","links":["`+link+`"],"can":`+allCan+`}}`)
	}
	put("1.0.0", "http://old/.*")
	r := e.open(t)
	if err := r.SetEnabled("usr", true, true); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.SourceForLink("http://old/x"); !ok {
		t.Fatal("old pattern does not match")
	}
	put("1.0.1", "http://new/.*")
	r.Refresh()
	if err := r.SetEnabled("usr", true, true); err != nil { // an edited copy needs the user's trust again
		t.Fatal(err)
	}
	if _, ok := r.SourceForLink("http://old/x"); ok {
		t.Fatal("old pattern still served after Refresh")
	}
	if _, ok := r.SourceForLink("http://new/x"); !ok {
		t.Fatal("new pattern does not match")
	}
}
