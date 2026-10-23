// Package genre looks up artist genres on chosic.com, caches them and writes them into FLAC tags.
package genre

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
)

const (
	chosicBase = "https://www.chosic.com"
	finderPage = chosicBase + "/music-genre-finder/"
	userAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"
	maxGenres  = 5
)

// Client talks to chosic's genre-finder API. The zero value uses the real site and http.DefaultClient.
type Client struct {
	Base string       // "" means https://www.chosic.com
	HTTP *http.Client // nil means http.DefaultClient; never mutated
}

// Genres returns up to 5 genres of the track's first artist, trimmed and deduped case-insensitively.
func (c *Client) Genres(ctx context.Context, spotifyTrackID string) ([]string, error) {
	id := strings.TrimSpace(spotifyTrackID)
	if id == "" {
		return nil, fmt.Errorf("empty track ID")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	hc := http.DefaultClient
	if c.HTTP != nil {
		hc = c.HTTP
	}
	cp := *hc // shallow copy: the shared client keeps no cookies
	cp.Jar = jar
	s := session{hc: &cp, api: strings.TrimRight(c.Base, "/") + "/api/tools"}
	if c.Base == "" {
		s.api = chosicBase + "/api/tools"
	}

	var hs struct {
		Success bool `json:"success"`
	}
	if err := s.do(ctx, http.MethodPost, "/handshake/", &hs); err != nil {
		return nil, err
	}
	if !hs.Success {
		return nil, fmt.Errorf("chosic handshake returned unsuccessful response")
	}

	var track struct {
		Artists []struct {
			ID string `json:"id"`
		} `json:"artists"`
	}
	if err := s.do(ctx, http.MethodGet, "/tracks/"+url.PathEscape(id), &track); err != nil {
		return nil, err
	}
	artistID := ""
	for _, a := range track.Artists {
		if artistID = strings.TrimSpace(a.ID); artistID != "" {
			break
		}
	}
	if artistID == "" {
		return nil, fmt.Errorf("chosic track response did not contain artist ID")
	}

	var artist struct {
		Genres []string `json:"genres"`
	}
	if err := s.do(ctx, http.MethodGet, "/artists/"+url.PathEscape(artistID), &artist); err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, g := range artist.Genres {
		g = strings.TrimSpace(g)
		if k := strings.ToLower(g); g != "" && !seen[k] {
			seen[k] = true
			if out = append(out, g); len(out) == maxGenres {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("chosic artist response did not contain genres")
	}
	return out, nil
}

type session struct {
	hc  *http.Client
	api string
}

func (s session) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, s.api+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Origin", chosicBase)
	req.Header.Set("Referer", finderPage)
	req.Header.Set("User-Agent", userAgent)
	resp, err := s.hc.Do(req)
	if err != nil {
		return fmt.Errorf("chosic %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("chosic %s: HTTP %d (%s)", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("chosic %s: decode: %w", path, err)
	}
	return nil
}
