package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Item is a track or album on either service.
type Item struct {
	Kind     string  `json:"kind"` // "track" or "album"
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Image    string  `json:"image"`
	URL      string  `json:"url"`
	Explicit bool    `json:"explicit"`
	Score    float64 `json:"score"`
}

var (
	spotifyURLRe  = regexp.MustCompile(`(?:open\.spotify\.com/(?:intl-[a-zA-Z-]+/)?|spotify:)(track|album)[/:]([A-Za-z0-9]{22})`)
	nextDataRe    = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)
	errBadSpotify = errors.New("это не ссылка на трек или альбом Spotify")
)

func parseSpotifyURL(s string) (kind, id string, err error) {
	m := spotifyURLRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", "", errBadSpotify
	}
	return m[1], m[2], nil
}

// fetchSpotify reads metadata from the public embed page, which needs no token.
func fetchSpotify(ctx context.Context, kind, id string) (Item, error) {
	body, err := httpDo(ctx, "GET", "https://open.spotify.com/embed/"+kind+"/"+id, nil, nil)
	if err != nil {
		return Item{}, fmt.Errorf("spotify: %w", err)
	}
	return parseSpotifyEmbed(body, kind, id)
}

func parseSpotifyEmbed(page []byte, kind, id string) (Item, error) {
	m := nextDataRe.FindSubmatch(page)
	if m == nil {
		return Item{}, errors.New("spotify: не удалось прочитать страницу")
	}
	var data struct {
		Props struct {
			PageProps struct {
				State struct {
					Data struct {
						Entity *struct {
							Name       string `json:"name"`
							Subtitle   string `json:"subtitle"`
							IsExplicit bool   `json:"isExplicit"`
							Artists    []struct {
								Name string `json:"name"`
							} `json:"artists"`
							TrackList []struct {
								IsExplicit bool `json:"isExplicit"`
							} `json:"trackList"`
							VisualIdentity struct {
								Image []struct {
									URL      string `json:"url"`
									MaxWidth int    `json:"maxWidth"`
								} `json:"image"`
							} `json:"visualIdentity"`
						} `json:"entity"`
					} `json:"data"`
				} `json:"state"`
			} `json:"pageProps"`
		} `json:"props"`
	}
	if err := json.Unmarshal(m[1], &data); err != nil {
		return Item{}, fmt.Errorf("spotify: %w", err)
	}
	e := data.Props.PageProps.State.Data.Entity
	if e == nil || e.Name == "" {
		return Item{}, errors.New("spotify: трек или альбом не найден")
	}
	it := Item{Kind: kind, Title: e.Name, Explicit: e.IsExplicit, URL: "https://open.spotify.com/" + kind + "/" + id}
	// The album-level flag is always false; an album is explicit if any track is.
	for _, tr := range e.TrackList {
		it.Explicit = it.Explicit || tr.IsExplicit
	}
	if len(e.Artists) > 0 {
		names := make([]string, len(e.Artists))
		for i, a := range e.Artists {
			names[i] = a.Name
		}
		it.Artist = strings.Join(names, ", ")
	} else {
		// Albums only carry a display string like "Pitbull, Sensato".
		it.Artist = strings.ReplaceAll(e.Subtitle, " ", " ")
	}
	best := 0
	for _, img := range e.VisualIdentity.Image {
		if img.MaxWidth > best {
			best, it.Image = img.MaxWidth, img.URL
		}
	}
	return it, nil
}
