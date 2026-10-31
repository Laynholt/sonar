package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"sonar/internal/download"
	"sonar/internal/match"
	"sonar/internal/model"
	"sonar/internal/source"
)

func putDownloader(t *testing.T, e *regEnv, id string) {
	t.Helper()
	e.bless(t, id, putFakeManifest(t, e.builtin, id, `{"id":"`+id+`","name":"Fake `+id+`","version":"1.0.0","kind":"downloader","run":["fake"],`+
		`"downloader":{"accepts":"amazon","parallel":2,"guard":{"limit":50,"waits":[15,30],"wait":30}}}`))
}

// Downloaders lists valid downloaders in Order, switched off or not; each one is a source.Source.
func TestDownloaders(t *testing.T) {
	e := newEnv(t)
	putDownloader(t, e, "dl1")
	putDownloader(t, e, "dl2")
	putSource(t, e, "src", "svc", `[]`, `[]`)
	r := e.open(t)
	if err := r.SetOrder([]string{"dl2", "dl1"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled("dl1", false, false); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range r.Downloaders() {
		got = append(got, d.ID)
	}
	if !slices.Equal(got, []string{"dl2", "dl1"}) {
		t.Fatalf("Downloaders = %v", got)
	}
	var s source.Source = r.Downloaders()[0]
	d := s.(*Downloader)
	if d.Name() != "dl2" || d.Label() != "Fake dl2" || d.Info.Accepts != "amazon" || d.Info.Parallel != 2 || d.Info.Guard.Limit != 50 {
		t.Fatalf("downloader %+v, label %q", d, d.Label())
	}
}

// Failures of the plugin process itself: a crash cools the site down, a closed or disabled plugin means the
// site is off (the track waits, no try counted), and a cancelled ctx stays a plain cancel.
func TestDownloaderAdapterHostErrors(t *testing.T) {
	e := newEnv(t)
	putDownloader(t, e, "dl")
	r := e.open(t)
	d := r.Downloaders()[0]
	bg := context.Background()

	_, err := d.Download(bg, source.Request{PathNoExt: "crash"})
	if !errors.Is(err, source.ErrUnavailable) || errors.Is(err, download.ErrSiteOff) {
		t.Fatalf("crash: %v", err)
	}
	ctx, cancel := context.WithCancel(bg)
	cancel()
	if _, err := d.Download(ctx, source.Request{PathNoExt: "p"}); err != context.Canceled {
		t.Fatalf("cancelled: %v", err) // the restart backoff error must not hide the cancel
	}

	h, err := r.Host("dl")
	if err != nil {
		t.Fatal(err)
	}
	h.Close()
	if _, err := d.Download(bg, source.Request{PathNoExt: "p"}); !errors.Is(err, download.ErrSiteOff) || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed: %v", err)
	}
	if err := r.SetEnabled("dl", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Download(bg, source.Request{PathNoExt: "p"}); !errors.Is(err, download.ErrSiteOff) || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("disabled: %v", err)
	}
	if got := (&Downloader{ID: "gone", r: r}).Label(); got != "gone" {
		t.Fatalf("Label of a missing plugin = %q", got)
	}
}

func TestDownloaderAdapterProgress(t *testing.T) {
	e := newEnv(t)
	putDownloader(t, e, "dl")
	d := e.open(t).Downloaders()[0]
	var got [][2]int64
	var statuses []string
	ctx := download.WithProgress(context.Background(), func(done, total int64) { got = append(got, [2]int64{done, total}) })
	base := filepath.Join(t.TempDir(), "p")
	path, err := d.Download(ctx, source.Request{Track: download.Track{Title: "T"}, IDs: map[string]string{"amazon": "B1"}, PathNoExt: base,
		OnStatus: func(s string) { statuses = append(statuses, s) }})
	if err != nil || path != base+"-T-B1.flac" {
		t.Fatalf("path %q, err %v", path, err)
	}
	if !reflect.DeepEqual(got, [][2]int64{{1, 3}, {3, 3}}) || !slices.Equal(statuses, []string{"Ripping"}) {
		t.Fatalf("progress %v, statuses %q", got, statuses)
	}
}

// A downloader's path must be an absolute regular file in pathNoExt's folder; anything else fails the track
// as an ordinary error, never one that pauses the site.
func TestDownloaderAdapterBadPath(t *testing.T) {
	e := newEnv(t)
	putDownloader(t, e, "dl")
	d := e.open(t).Downloaders()[0]
	_, err := d.Download(context.Background(), source.Request{Track: download.Track{Title: "T"}, PathNoExt: "p"}) // the fake answers p-T-.flac
	if err == nil || !strings.Contains(err.Error(), "not an absolute path") || errors.Is(err, source.ErrUnavailable) ||
		errors.Is(err, source.ErrNeedsCheck) || errors.Is(err, download.ErrSiteOff) {
		t.Fatalf("relative path: %v", err)
	}
}

func TestCheckPath(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "a")
	file := base + ".flac"
	other := filepath.Join(dir, "sub")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "a.flac"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{
		{file, ""},
		{"a.flac", "not an absolute path"},
		{base + ".mp3", "not a file"},
		{other, "not a file"},
		{filepath.Join(other, "a.flac"), "not in the folder"},
	} {
		err := checkPath(tc.path, base)
		if (tc.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("checkPath(%q) = %v, want %q", tc.path, err, tc.want)
		}
	}
}

// The contract's codes become the source errors the Balancer acts on, keeping the plugin's text;
// through a Balancer, a needs-action error shows the plugin's action.
func TestDownloaderAdapterErrors(t *testing.T) {
	e := newEnv(t)
	putDownloader(t, e, "dl")
	d := e.open(t).Downloaders()[0]
	ctx := context.Background()
	for _, tc := range []struct {
		path, msg   string
		unavailable bool
		needs       bool
	}{{"unavailable", "fake: down", true, false}, {"needs", "fake: log in", false, true}, {"boom", "fake: boom", false, false}} {
		_, err := d.Download(ctx, source.Request{PathNoExt: tc.path})
		var pe *Error
		if err == nil || err.Error() != tc.msg || errors.Is(err, source.ErrUnavailable) != tc.unavailable ||
			errors.Is(err, source.ErrNeedsCheck) != tc.needs || !errors.As(err, &pe) {
			t.Errorf("%s: err = %v", tc.path, err)
		}
	}

	b := source.NewBalancer(time.Minute)
	b.Add(d, d.Info.Accepts, source.Guard{})
	if _, err := b.Fetch(ctx, "dl", source.Request{PathNoExt: "needs"}); err == nil || err.Error() != "dl: fake: log in" {
		t.Fatalf("Fetch: %v", err)
	}
	if st := b.Status(); len(st) != 1 || !st[0].NeedsAction || st[0].Action != "login" || st[0].Reason != "Fake dl: Log in" || st[0].Label != "Fake dl" || st[0].Accepts != "amazon" {
		t.Fatalf("Status = %+v", st)
	}
}

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
