// Package resolve finds, for tracks of one service, the matching tracks on another.
package resolve

import (
	"cmp"
	"context"
	"log"
	"slices"

	"sonar/internal/download"
	"sonar/internal/match"
	"sonar/internal/model"
)

// MaxCandidates is how many target tracks a review offers.
const MaxCandidates = 4

// Finder is the network the resolver needs for one pair of services: the first three are the target
// source, OriginIDsByISRC is the origin source.
type Finder interface {
	Search(ctx context.Context, query string) ([]match.Item, error)
	LookupTracks(ctx context.Context, ids []string) ([]model.TrackInfo, error)
	LookupAlbum(ctx context.Context, id string) (*model.AlbumInfo, error)
	OriginIDsByISRC(ctx context.Context, isrc string) ([]string, error)
}

// Finders builds the Finder for tracks of service from matched on service to.
// It errors when either source is missing, disabled or lacks a capability.
type Finders func(from, to string) (Finder, error)

type Resolver struct {
	f Finders
	c *Cache
}

func New(f Finders, c *Cache) *Resolver { return &Resolver{f: f, c: c} }

// Resolve is a download.Resolve: one Resolution per track, same order, matching the tracks to service to.
// The tracks of a call share one service. Resolution{} means nothing was found.
func (r *Resolver) Resolve(ctx context.Context, to string, tracks []download.Track) []download.Resolution {
	out := make([]download.Resolution, len(tracks))
	if len(tracks) == 0 {
		return out
	}
	from := download.ServiceOf(tracks[0])
	var todo []int
	for i, t := range tracks {
		if id, _, ok := r.c.Get(from, to, t.ID); ok {
			out[i] = download.Resolution{ID: id, Via: "cache"}
		} else {
			todo = append(todo, i)
		}
	}
	if len(todo) > 0 {
		f, err := r.f(from, to)
		if err != nil {
			for _, i := range todo {
				out[i].Err = err
			}
			return out
		}
		if len(tracks) > 1 || tracks[0].Group != "" {
			todo = r.album(ctx, f, tracks, todo, out)
		}
		for _, i := range todo {
			out[i] = r.track(ctx, f, tracks[i])
		}
	}
	for i, res := range out {
		if res.ID != "" && res.Via != "cache" {
			r.c.Put(from, to, tracks[i].ID, res.ID, res.Via)
		}
	}
	return out
}

// album matches the tracks at todo as one target album and returns the indexes left for the track path.
func (r *Resolver) album(ctx context.Context, f Finder, tracks []download.Track, todo []int, out []download.Resolution) []int {
	fail := func(err error) []int {
		for _, i := range todo {
			out[i].Err = err
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	t0 := tracks[todo[0]]
	src := match.Item{Kind: "album", Title: t0.Album, Artist: cmp.Or(t0.AlbumArtist, t0.Artist)}
	for _, i := range todo {
		src.Explicit = src.Explicit || tracks[i].Explicit
	}
	items, err := f.Search(ctx, match.FirstArtist(src.Artist)+" "+match.BaseTitle(src.Title))
	if err != nil {
		return fail(err)
	}
	best := match.Rank(src, items, 3)
	if len(best) == 0 || best[0].Score < match.ConfidentScore {
		return todo
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	al, err := f.LookupAlbum(ctx, best[0].ID)
	if err != nil {
		log.Printf("resolve: album %s: %v; matching track by track", best[0].ID, err)
		return todo
	}
	var left []int
	for _, i := range todo {
		if at := align(tracks[i], al.Tracks); at.ID != "" {
			out[i] = download.Resolution{ID: at.ID, Via: "album", ISRC: at.ISRC}
		} else {
			left = append(left, i)
		}
	}
	return left
}

// align picks the album track that is confidently t: the same track number first, then the best score.
// The zero TrackInfo means none.
func align(t download.Track, album []model.TrackInfo) model.TrackInfo {
	src := match.Item{Title: t.Title, Artist: t.Artist, Explicit: t.Explicit}
	var best model.TrackInfo
	bestSame, bestScore := false, 0.0
	for _, at := range album {
		s := match.Score(src, match.Item{Title: at.Title, Artist: at.Artist, Explicit: at.Explicit})
		if s < match.ConfidentScore || !match.DurationClose(t.DurationMS, at.DurationMS) {
			continue
		}
		same := t.TrackNumber > 0 && t.TrackNumber == at.TrackNumber &&
			(t.DiscNumber == 0 || at.DiscNumber == 0 || t.DiscNumber == at.DiscNumber)
		if best.ID == "" || same && !bestSame || same == bestSame && s > bestScore {
			best, bestSame, bestScore = at, same, s
		}
	}
	return best
}

type candidate struct {
	download.Candidate
	isrc  string
	rank  float64
	known bool // LookupTracks returned it, so its duration is real
}

func (r *Resolver) track(ctx context.Context, f Finder, t download.Track) download.Resolution {
	if err := ctx.Err(); err != nil {
		return download.Resolution{Err: err}
	}
	src := match.Item{Kind: "track", Title: t.Title, Artist: t.Artist, Explicit: t.Explicit}
	items, err := f.Search(ctx, match.FirstArtist(t.Artist)+" "+match.BaseTitle(t.Title))
	if err != nil {
		return download.Resolution{Err: err}
	}
	ranked := match.Rank(src, items, MaxCandidates)
	if len(ranked) == 0 {
		return download.Resolution{}
	}
	if err := ctx.Err(); err != nil {
		return download.Resolution{Err: err}
	}
	ids := make([]string, len(ranked))
	for i, it := range ranked {
		ids[i] = it.ID
	}
	infos, err := f.LookupTracks(ctx, ids)
	if err != nil {
		return download.Resolution{Err: err}
	}
	byID := map[string]model.TrackInfo{}
	for _, ti := range infos {
		byID[ti.ID] = ti
	}
	cands := make([]candidate, len(ranked))
	for i, it := range ranked {
		c := candidate{Candidate: download.Candidate{ID: it.ID, Title: it.Title, Artist: it.Artist, Image: it.Image, Explicit: it.Explicit, Score: it.Score}}
		if ti, ok := byID[it.ID]; ok {
			c.Title, c.Artist = cmp.Or(ti.Title, it.Title), cmp.Or(ti.Artist, it.Artist)
			c.Album, c.ReleaseDate, c.Image = ti.Album, ti.ReleaseDate, cmp.Or(ti.Image, it.Image)
			c.DurationMS, c.Explicit, c.isrc, c.known = ti.DurationMS, ti.Explicit, ti.ISRC, true
		}
		c.rank = c.Score
		if match.SameAlbum(t.Album, c.Album) {
			c.rank += 0.03 // reorders only: confidence is checked on the plain Score
		}
		cands[i] = c
	}
	slices.SortStableFunc(cands, func(a, b candidate) int { return cmp.Compare(b.rank, a.rank) })

	if top := cands[0]; top.known && top.Score >= match.ConfidentScore && match.DurationClose(t.DurationMS, top.DurationMS) {
		return download.Resolution{ID: top.ID, Via: "title", ISRC: top.isrc}
	}
	for _, c := range cands {
		if c.isrc == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return download.Resolution{Err: err}
		}
		origin, err := f.OriginIDsByISRC(ctx, c.isrc)
		if err != nil {
			log.Printf("resolve: ISRC %s: %v", c.isrc, err)
			continue
		}
		if slices.Contains(origin, t.ID) {
			return download.Resolution{ID: c.ID, Via: "isrc", ISRC: c.isrc}
		}
	}
	res := download.Resolution{Candidates: make([]download.Candidate, len(cands))}
	for i, c := range cands {
		res.Candidates[i] = c.Candidate
	}
	res.Candidates[0].Recommended = true
	return res
}
