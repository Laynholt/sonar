package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sonar/internal/history"
	"sonar/internal/httpx"
	"sonar/internal/model"
	"sonar/internal/spotify"
)

type App struct {
	ctx     context.Context
	dataDir string // holds history.db and config.json
	hist    *history.DB
}

var errNoHistory = errors.New("history is unavailable")

func NewApp() (*App, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &App{dataDir: filepath.Join(dir, "sonar")}, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		log.Printf("data dir: %v", err)
	}
	// A second instance holds the lock; the app still runs, without history.
	if h, err := history.Open(filepath.Join(a.dataDir, "history.db")); err != nil {
		log.Printf("history: %v", err)
	} else {
		a.hist = h
	}
	s, err := a.LoadSettings()
	if err != nil {
		log.Printf("settings: %v", err)
	}
	proxy, _ := s["proxy"].(string)
	if err := httpx.SetManualProxy(proxy); err != nil {
		log.Printf("saved proxy: %v", err)
	}
}

func (a *App) shutdown(context.Context) {
	if a.hist != nil {
		a.hist.Close()
	}
}

// GetMetadata returns the JSON of a Spotify track, album, playlist or artist.
func (a *App) GetMetadata(url string) (string, error) {
	if strings.Contains(url, "music.amazon.") {
		return "", errors.New("Amazon Music links are not supported yet")
	}
	if !spotify.IsLink(url) {
		return "", errors.New("Unsupported link. Paste a Spotify link.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	data, err := spotify.GetMetadata(ctx, url)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(data)
	return string(b), err
}

type SpotifySearchRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

func (a *App) SearchSpotify(req SpotifySearchRequest) (*model.SearchResponse, error) {
	if req.Query == "" {
		return nil, errors.New("search query is required")
	}
	if req.Limit <= 0 {
		req.Limit = 10
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return spotify.Search(ctx, req.Query, req.Limit)
}

type SpotifySearchByTypeRequest struct {
	Query      string `json:"query"`
	SearchType string `json:"search_type"`
	Limit      int    `json:"limit"`
	Offset     int    `json:"offset"`
}

func (a *App) SearchSpotifyByType(req SpotifySearchByTypeRequest) ([]model.SearchResult, error) {
	if req.Query == "" {
		return nil, errors.New("search query is required")
	}
	if req.SearchType == "" {
		return nil, errors.New("search type is required")
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return spotify.SearchByType(ctx, req.Query, req.SearchType, req.Limit, req.Offset)
}

func (a *App) GetPreviewURL(trackID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return spotify.PreviewURL(ctx, trackID)
}

func (a *App) configPath() string { return filepath.Join(a.dataDir, "config.json") }

// SaveSettings validates the proxy first, so a bad value writes nothing.
func (a *App) SaveSettings(s map[string]any) error {
	proxy, _ := s["proxy"].(string)
	if err := httpx.SetManualProxy(proxy); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(a.dataDir, "config-*.tmp")
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(f.Name(), a.configPath())
	}
	if werr != nil {
		os.Remove(f.Name())
		return fmt.Errorf("save settings: %w", werr)
	}
	return nil
}

// LoadSettings returns nil when nothing has been saved yet.
func (a *App) LoadSettings() (map[string]any, error) {
	data, err := os.ReadFile(a.configPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s map[string]any
	return s, json.Unmarshal(data, &s)
}

func (a *App) GetFetchHistory() ([]history.FetchItem, error) {
	if a.hist == nil {
		return nil, errNoHistory
	}
	return a.hist.List()
}

func (a *App) AddFetchHistory(item history.FetchItem) error {
	if a.hist == nil {
		return errNoHistory
	}
	return a.hist.Add(item)
}

func (a *App) DeleteFetchHistoryItem(id string) error {
	if a.hist == nil {
		return errNoHistory
	}
	return a.hist.Delete(id)
}

func (a *App) ClearFetchHistory() error {
	if a.hist == nil {
		return errNoHistory
	}
	return a.hist.Clear()
}

func (a *App) ClearFetchHistoryByType(itemType string) error {
	if a.hist == nil {
		return errNoHistory
	}
	return a.hist.ClearByType(itemType)
}
