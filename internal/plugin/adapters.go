package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"sonar/internal/match"
	"sonar/internal/model"
)

// ErrCannot is what a source method answers when the manifest's source.can lacks it.
var ErrCannot = errors.New("source cannot do that")

// SourceClient is the typed face of an enabled source plugin. It looks the Host up per call,
// so a plugin replaced by Refresh is never talked to through a stale Host.
type SourceClient struct {
	ID      string // plugin id
	Service string
	Can     []string
	r       *Registry
}

func newSourceClient(r *Registry, e Entry) *SourceClient {
	return &SourceClient{ID: e.M.ID, Service: e.M.Source.Service, Can: e.M.Source.Can, r: r}
}

// Source returns the first enabled source in Order that serves service.
func (r *Registry) Source(service string) (*SourceClient, error) {
	for _, e := range r.Enabled(KindSource) {
		if e.M.Source != nil && e.M.Source.Service == service {
			return newSourceClient(r, e), nil
		}
	}
	return nil, fmt.Errorf("no enabled source for service %q", service)
}

// SourceForLink returns the first enabled source in Order with a source.links pattern that matches url.
func (r *Registry) SourceForLink(url string) (*SourceClient, bool) {
	for _, e := range r.Enabled(KindSource) {
		if e.M.Source == nil {
			continue
		}
		for _, re := range r.linkRes(e) {
			if re.MatchString(url) {
				return newSourceClient(r, e), true
			}
		}
	}
	return nil, false
}

// linkRec holds the compiled links of one copy of a plugin; sum says which copy.
type linkRec struct {
	sum string
	res []*regexp.Regexp
}

// linkRes compiles e's source.links once per copy (a changed copy has another checksum).
// A pattern that does not compile is logged and never matches.
func (r *Registry) linkRes(e Entry) []*regexp.Regexp {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.links[e.M.ID]; ok && rec.sum == e.Sum {
		return rec.res
	}
	var res []*regexp.Regexp
	for _, p := range e.M.Source.Links {
		if re, err := regexp.Compile(p); err != nil {
			r.logf("plugin %q: source.links %q: %v", e.M.ID, p, err)
		} else {
			res = append(res, re)
		}
	}
	if r.links == nil {
		r.links = map[string]linkRec{}
	}
	r.links[e.M.ID] = linkRec{e.Sum, res}
	return res
}

// call runs method (one of the source.* constants) with a timeout, after checking the capability.
// The host is not fetched, so the process is not started, when the source cannot do it.
func (s *SourceClient) call(ctx context.Context, method string, timeout time.Duration, params, result any) error {
	if !slices.Contains(s.Can, strings.TrimPrefix(method, "source.")) {
		return ErrCannot
	}
	h, err := s.r.Host(s.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return h.Call(ctx, method, params, result, nil)
}

func (s *SourceClient) Metadata(ctx context.Context, url string) (json.RawMessage, error) {
	var out json.RawMessage
	err := s.call(ctx, MSourceMetadata, 120*time.Second, struct {
		URL string `json:"url"`
	}{url}, &out)
	return out, err
}

func (s *SourceClient) Search(ctx context.Context, query string, limit int) (*model.SearchResponse, error) {
	var out model.SearchResponse
	err := s.call(ctx, MSourceSearch, 60*time.Second, struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}{query, limit}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *SourceClient) SearchByType(ctx context.Context, query, typ string, limit, offset int) ([]model.SearchResult, error) {
	var out []model.SearchResult
	err := s.call(ctx, MSourceSearchByType, 60*time.Second, struct {
		Query  string `json:"query"`
		Type   string `json:"type"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}{query, typ, limit, offset}, &out)
	return out, err
}

func (s *SourceClient) Find(ctx context.Context, query string) ([]match.Item, error) {
	var out []match.Item
	err := s.call(ctx, MSourceFind, 30*time.Second, struct {
		Query string `json:"query"`
	}{query}, &out)
	return out, err
}

func (s *SourceClient) Tracks(ctx context.Context, ids []string) ([]model.TrackInfo, error) {
	var out []model.TrackInfo
	err := s.call(ctx, MSourceTracks, 30*time.Second, struct {
		IDs []string `json:"ids"`
	}{ids}, &out)
	return out, err
}

func (s *SourceClient) Album(ctx context.Context, id string) (*model.AlbumInfo, error) {
	var out model.AlbumInfo
	err := s.call(ctx, MSourceAlbum, 30*time.Second, struct {
		ID string `json:"id"`
	}{id}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *SourceClient) ISRC(ctx context.Context, isrc string) ([]string, error) {
	var out []string
	err := s.call(ctx, MSourceISRC, 30*time.Second, struct {
		ISRC string `json:"isrc"`
	}{isrc}, &out)
	return out, err
}

func (s *SourceClient) Preview(ctx context.Context, id string) (string, error) {
	var out string
	err := s.call(ctx, MSourcePreview, 30*time.Second, struct {
		ID string `json:"id"`
	}{id}, &out)
	return out, err
}

func (s *SourceClient) ParseLink(ctx context.Context, url string) (Link, error) {
	var out Link
	err := s.call(ctx, MSourceParseLink, 30*time.Second, struct {
		URL string `json:"url"`
	}{url}, &out)
	return out, err
}

// TaggerClient is the typed face of an enabled tagger plugin.
type TaggerClient struct {
	ID     string
	Fields []string // the tags it can fill
	Needs  string   // a service whose ID it wants, or ""
	r      *Registry
}

// Taggers lists the enabled taggers in Order.
func (r *Registry) Taggers() []*TaggerClient {
	var out []*TaggerClient
	for _, e := range r.Enabled(KindTagger) {
		if e.M.Tagger != nil {
			out = append(out, &TaggerClient{ID: e.M.ID, Fields: e.M.Tagger.Fields, Needs: e.M.Tagger.Needs, r: r})
		}
	}
	return out
}

// Tag has no timeout of its own: the caller's ctx sets the budget.
func (t *TaggerClient) Tag(ctx context.Context, p TagParams) (map[string]string, error) {
	h, err := t.r.Host(t.ID)
	if err != nil {
		return nil, err
	}
	var out TagResult
	if err := h.Call(ctx, MTag, p, &out, nil); err != nil {
		return nil, err
	}
	return out.Fields, nil
}
