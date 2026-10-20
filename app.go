package main

import (
	"context"
	"time"
)

type App struct{}

type Result struct {
	Source    Item   `json:"source"`
	Matches   []Item `json:"matches"` // best first; may be empty
	Confident bool   `json:"confident"`
	SearchURL string `json:"searchUrl"` // Amazon search page for manual lookup
}

// Find looks up a Spotify track or album link on Amazon Music.
func (a *App) Find(link string) (Result, error) {
	kind, id, err := parseSpotifyURL(link)
	if err != nil {
		return Result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	src, err := fetchSpotify(ctx, kind, id)
	if err != nil {
		return Result{}, err
	}
	query := firstArtist(src.Artist) + " " + baseTitle(src.Title)
	res := Result{Source: src, SearchURL: amazonSearchURL(query)}

	found, err := searchAmazon(ctx, query)
	if err != nil {
		return Result{}, err
	}
	res.Matches = rank(src, found, 4)
	res.Confident = len(res.Matches) > 0 && res.Matches[0].Score >= confidentScore
	return res, nil
}
