package resolve

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"sonar/internal/download"
	"sonar/internal/match"
	"sonar/internal/model"
)

// fakeFinder answers from maps and records every call as "method arg".
type fakeFinder struct {
	search    map[string][]match.Item
	searchErr error
	tracks    map[string]model.TrackInfo
	tracksErr error
	albums    map[string]*model.AlbumInfo
	isrc      map[string][]string
	isrcErr   error
	calls     []string
}

func (f *fakeFinder) Search(_ context.Context, q string) ([]match.Item, error) {
	f.calls = append(f.calls, "Search "+q)
	return f.search[q], f.searchErr
}

func (f *fakeFinder) LookupTracks(_ context.Context, ids []string) ([]model.TrackInfo, error) {
	f.calls = append(f.calls, "LookupTracks")
	if f.tracksErr != nil {
		return nil, f.tracksErr
	}
	var out []model.TrackInfo
	for _, id := range ids {
		if t, ok := f.tracks[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func (f *fakeFinder) LookupAlbum(_ context.Context, id string) (*model.AlbumInfo, error) {
	f.calls = append(f.calls, "LookupAlbum "+id)
	if a, ok := f.albums[id]; ok {
		return a, nil
	}
	return nil, errors.New("no album " + id)
}

func (f *fakeFinder) SpotifyIDsByISRC(_ context.Context, isrc string) ([]string, error) {
	f.calls = append(f.calls, "ISRC "+isrc)
	return f.isrc[isrc], f.isrcErr
}

func (f *fakeFinder) count(prefix string) int {
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func newCache(t *testing.T) *Cache {
	return OpenCache(filepath.Join(t.TempDir(), "matches.json"))
}

var cruel = download.Track{ID: "sp1", Title: "Cruel Summer", Artist: "Taylor Swift", Album: "Lover", AlbumArtist: "Taylor Swift", DurationMS: 178426, Service: "spotify"}

func trackItem(id, title, artist string) match.Item {
	return match.Item{Kind: "track", ID: id, Title: title, Artist: artist}
}

func TestResolveConfidentTitle(t *testing.T) {
	f := &fakeFinder{
		search: map[string][]match.Item{"Taylor Swift Cruel Summer": {trackItem("A1", "Cruel Summer", "Taylor Swift")}},
		tracks: map[string]model.TrackInfo{"A1": {ID: "A1", Title: "Cruel Summer", Artist: "Taylor Swift", Album: "Lover", DurationMS: 180000}},
	}
	c := newCache(t)
	got := New(f, c).Resolve(context.Background(), []download.Track{cruel})
	if got[0].ID != "A1" || got[0].Via != "title" || got[0].Err != nil {
		t.Fatalf("got %+v", got[0])
	}
	if id, via, ok := c.Get("sp1"); !ok || id != "A1" || via != "title" {
		t.Errorf("cache = %q %q %v", id, via, ok)
	}
}

func TestResolveLiveVersionNeedsReview(t *testing.T) {
	live := "Cruel Summer (Live From TS The Eras Tour)"
	f := &fakeFinder{
		search: map[string][]match.Item{"Taylor Swift Cruel Summer": {trackItem("L1", live, "Taylor Swift")}},
		tracks: map[string]model.TrackInfo{"L1": {ID: "L1", Title: live, Artist: "Taylor Swift", Album: "The Eras Tour", DurationMS: 193000}},
	}
	c := newCache(t)
	got := New(f, c).Resolve(context.Background(), []download.Track{cruel})[0]
	if got.ID != "" || got.Err != nil || len(got.Candidates) != 1 || !got.Candidates[0].Recommended {
		t.Fatalf("got %+v", got)
	}
	if cd := got.Candidates[0]; cd.ID != "L1" || cd.Album != "The Eras Tour" || cd.DurationMS != 193000 {
		t.Errorf("candidate not filled from Amazon: %+v", cd)
	}
	if _, _, ok := c.Get("sp1"); ok {
		t.Error("a review must not be cached")
	}
}

func isrcFinder() *fakeFinder {
	return &fakeFinder{
		search: map[string][]match.Item{"Taylor Swift Cruel Summer": {trackItem("I1", "Cruel Summer", "Various Artists")}},
		tracks: map[string]model.TrackInfo{"I1": {ID: "I1", Title: "Cruel Summer", Artist: "Various Artists", ISRC: "X", DurationMS: 178000}},
		isrc:   map[string][]string{"X": {"other", "sp1"}},
	}
}

func TestResolveISRCConfirms(t *testing.T) {
	f := isrcFinder()
	got := New(f, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if got.ID != "I1" || got.Via != "isrc" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveISRCErrorFallsBackToReview(t *testing.T) {
	f := isrcFinder()
	f.isrcErr = errors.New("spotify down")
	got := New(f, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if got.ID != "" || got.Err != nil || len(got.Candidates) != 1 || got.Candidates[0].ID != "I1" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveNotFound(t *testing.T) {
	got := New(&fakeFinder{}, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if got.ID != "" || got.Candidates != nil || got.Err != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestResolvePrefersSameAlbum(t *testing.T) {
	// Both lose the artist, so neither is confident; the Lover one must lead the review.
	f := &fakeFinder{
		search: map[string][]match.Item{"Taylor Swift Cruel Summer": {
			trackItem("C1", "Cruel Summer", "Various Artists"),
			trackItem("C2", "Cruel Summer", "Various Artists"),
		}},
		tracks: map[string]model.TrackInfo{
			"C1": {ID: "C1", Title: "Cruel Summer", Album: "Summer Hits 2023"},
			"C2": {ID: "C2", Title: "Cruel Summer", Album: "Lover (Deluxe)"},
		},
	}
	got := New(f, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if len(got.Candidates) != 2 || got.Candidates[0].ID != "C2" || !got.Candidates[0].Recommended || got.Candidates[1].Recommended {
		t.Fatalf("got %+v", got)
	}
}

func loverGroup() []download.Track {
	mk := func(id, title string, n, ms int) download.Track {
		return download.Track{ID: id, Title: title, Artist: "Taylor Swift", Album: "Lover", AlbumArtist: "Taylor Swift",
			TrackNumber: n, DurationMS: ms, Service: "spotify", AlbumID: "spAlbum", Group: "album:spAlbum"}
	}
	return []download.Track{mk("s1", "I Forgot That You Existed", 1, 170640), mk("s2", "Cruel Summer", 2, 178426), mk("s3", "Lover", 3, 221306)}
}

func loverFinder() *fakeFinder {
	at := func(id, title string, n, ms int) model.TrackInfo {
		return model.TrackInfo{ID: id, Title: title, Artist: "Taylor Swift", Album: "Lover", AlbumID: "ALB", TrackNumber: n, DurationMS: ms}
	}
	return &fakeFinder{
		search: map[string][]match.Item{"Taylor Swift Lover": {
			{Kind: "album", ID: "ALB", AlbumID: "ALB", Title: "Lover", Artist: "Taylor Swift"},
			trackItem("T3", "Lover", "Taylor Swift"),
		}},
		albums: map[string]*model.AlbumInfo{"ALB": {ID: "ALB", Title: "Lover", Artist: "Taylor Swift", Tracks: []model.TrackInfo{
			at("T1", "I Forgot That You Existed", 1, 171000), at("T2", "Cruel Summer", 2, 178000), at("T3", "Lover", 3, 221000),
		}}},
	}
}

func TestResolveAlbumAligns(t *testing.T) {
	f := loverFinder()
	c := newCache(t)
	got := New(f, c).Resolve(context.Background(), loverGroup())
	for i, want := range []string{"T1", "T2", "T3"} {
		if got[i].ID != want || got[i].Via != "album" {
			t.Errorf("track %d: got %+v, want %s via album", i, got[i], want)
		}
	}
	if f.count("Search") != 1 || f.count("LookupAlbum") != 1 || f.count("LookupTracks") != 0 {
		t.Errorf("calls = %v", f.calls)
	}
	if _, via, ok := c.Get("s3"); !ok || via != "album" {
		t.Errorf("album match not cached: %q %v", via, ok)
	}
}

func TestResolveAlbumUnalignedFallsBack(t *testing.T) {
	f := loverFinder()
	al := f.albums["ALB"]
	al.Tracks = []model.TrackInfo{al.Tracks[0], al.Tracks[2]} // "Cruel Summer" is missing from the album
	f.search["Taylor Swift Cruel Summer"] = []match.Item{trackItem("T2", "Cruel Summer", "Taylor Swift")}
	f.tracks = map[string]model.TrackInfo{"T2": {ID: "T2", Title: "Cruel Summer", Artist: "Taylor Swift", DurationMS: 178000}}
	got := New(f, newCache(t)).Resolve(context.Background(), loverGroup())
	if got[0].Via != "album" || got[2].Via != "album" || got[1].ID != "T2" || got[1].Via != "title" {
		t.Fatalf("got %+v", got)
	}
	want := []string{"Search Taylor Swift Lover", "LookupAlbum ALB", "Search Taylor Swift Cruel Summer", "LookupTracks"}
	if strings.Join(f.calls, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

func TestResolveCacheHit(t *testing.T) {
	f := &fakeFinder{}
	c := newCache(t)
	c.Put("sp1", "A9", "title")
	got := New(f, c).Resolve(context.Background(), []download.Track{cruel})[0]
	if got.ID != "A9" || got.Via != "cache" || len(f.calls) != 0 {
		t.Fatalf("got %+v, calls %v", got, f.calls)
	}
}

func TestResolveAlbumAllCachedNoCalls(t *testing.T) {
	f := loverFinder()
	c := newCache(t)
	for _, tr := range loverGroup() {
		c.Put(tr.ID, "X"+tr.ID, "album")
	}
	got := New(f, c).Resolve(context.Background(), loverGroup())
	if got[2].Via != "cache" || len(f.calls) != 0 {
		t.Fatalf("got %+v, calls %v", got, f.calls)
	}
}

func TestResolveSearchErrorSetsErr(t *testing.T) {
	boom := errors.New("boom")
	got := New(&fakeFinder{searchErr: boom}, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if !errors.Is(got.Err, boom) || got.ID != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveCancelledMakesNoCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := loverFinder()
	got := New(f, newCache(t)).Resolve(ctx, loverGroup())
	if len(f.calls) != 0 || !errors.Is(got[0].Err, context.Canceled) {
		t.Fatalf("got %+v, calls %v", got, f.calls)
	}
}

func TestResolveAlbumPrefersTrackNumber(t *testing.T) {
	f := loverFinder()
	al := f.albums["ALB"]
	al.Tracks = append([]model.TrackInfo{{ID: "T2bonus", Title: "Cruel Summer", Artist: "Taylor Swift", TrackNumber: 19, DurationMS: 178000}}, al.Tracks...)
	got := New(f, newCache(t)).Resolve(context.Background(), loverGroup())
	if got[1].ID != "T2" {
		t.Fatalf("got %+v, want the same track number T2", got[1])
	}
}

func TestResolveUnlookedTopIsNotConfident(t *testing.T) {
	// The lookup lost the live take, so its 193 s duration is unknown: that must not pass the guard.
	f := &fakeFinder{search: map[string][]match.Item{"Taylor Swift Cruel Summer": {
		trackItem("L1", "Cruel Summer (Live From TS The Eras Tour)", "Taylor Swift")}}}
	got := New(f, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if got.ID != "" || got.Err != nil || len(got.Candidates) != 1 || got.Candidates[0].ID != "L1" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveAlbumLookupErrorFallsBackToTrack(t *testing.T) {
	tr := cruel
	tr.Group = "album:spAlbum"
	f := &fakeFinder{
		search: map[string][]match.Item{
			"Taylor Swift Lover":        {{Kind: "album", ID: "GONE", Title: "Lover", Artist: "Taylor Swift"}},
			"Taylor Swift Cruel Summer": {trackItem("A1", "Cruel Summer", "Taylor Swift")},
		},
		tracks: map[string]model.TrackInfo{"A1": {ID: "A1", Title: "Cruel Summer", Artist: "Taylor Swift", DurationMS: 178000}},
	}
	got := New(f, newCache(t)).Resolve(context.Background(), []download.Track{tr})[0]
	if got.ID != "A1" || got.Via != "title" || got.Err != nil {
		t.Fatalf("got %+v", got)
	}
	if f.count("LookupAlbum GONE") != 1 {
		t.Errorf("calls = %v", f.calls)
	}
}

func TestResolveLookupTracksErrorSetsErr(t *testing.T) {
	boom := errors.New("boom")
	f := &fakeFinder{
		search:    map[string][]match.Item{"Taylor Swift Cruel Summer": {trackItem("A1", "Cruel Summer", "Taylor Swift")}},
		tracksErr: boom,
	}
	got := New(f, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if !errors.Is(got.Err, boom) || got.ID != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestResolveISRCRankOrderStopsAtFirst(t *testing.T) {
	// Equal low scores keep search order: J1 has no ISRC, J2 does not confirm, J3 and J4 both would.
	f := &fakeFinder{
		search: map[string][]match.Item{"Taylor Swift Cruel Summer": {
			trackItem("J1", "Cruel Summer", "Various Artists"), trackItem("J2", "Cruel Summer", "Various Artists"),
			trackItem("J3", "Cruel Summer", "Various Artists"), trackItem("J4", "Cruel Summer", "Various Artists"),
		}},
		tracks: map[string]model.TrackInfo{
			"J1": {ID: "J1"}, "J2": {ID: "J2", ISRC: "Y"}, "J3": {ID: "J3", ISRC: "Z"}, "J4": {ID: "J4", ISRC: "W"},
		},
		isrc: map[string][]string{"Y": {"other"}, "Z": {"sp1"}, "W": {"sp1"}},
	}
	got := New(f, newCache(t)).Resolve(context.Background(), []download.Track{cruel})[0]
	if got.ID != "J3" || got.Via != "isrc" {
		t.Fatalf("got %+v", got)
	}
	if want := "ISRC Y|ISRC Z"; strings.Join(f.calls[2:], "|") != want {
		t.Errorf("calls = %q, want ISRC calls %s", f.calls, want)
	}
}
