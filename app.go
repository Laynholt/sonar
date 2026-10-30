package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	bolt "go.etcd.io/bbolt"

	"sonar/internal/amazon"
	"sonar/internal/convert"
	"sonar/internal/doubledouble"
	"sonar/internal/download"
	"sonar/internal/ffinstall"
	"sonar/internal/genre"
	"sonar/internal/history"
	"sonar/internal/httpx"
	"sonar/internal/lucida"
	"sonar/internal/model"
	"sonar/internal/resolve"
	"sonar/internal/source"
	"sonar/internal/spotify"
)

type App struct {
	ctx     context.Context
	dataDir string // holds history.db, config.json, queue.json, matches.json and genres.json
	hist    *history.DB
	second  bool // another instance holds history.db, and owns queue.json: this one neither restores nor saves it

	dd            *doubledouble.Client
	sources       *source.Balancer
	dl            *download.Queue
	cache         *resolve.Cache // matches.json, shared with the resolver
	genreCache    *genre.Cache   // genres.json
	genreClient   genre.Client
	genres        func(ctx context.Context, t download.Track, amazonID string) ([]string, error) // a field so tests can fake it
	findFFmpeg    func(custom string) string                                                     // a field so tests can fake it
	amazonPreview func(ctx context.Context, id string) (string, error)                           // a field so tests can fake it

	ffLatest  func(context.Context, *http.Client, string) (ffinstall.Asset, ffinstall.Asset, error) // fields so tests can fake them
	ffInstall func(context.Context, *http.Client, ffinstall.Asset, ffinstall.Asset, string, func(ffinstall.Progress)) (string, error)
	ffMu      sync.Mutex // guards the two fields below
	ffState   FFmpegInstall
	ffCancel  context.CancelFunc // non-nil while an install runs

	saveCh    chan struct{} // cap 1: a pending request to save queue.json
	saveStop  chan struct{} // closed to stop the saver
	saverDone chan struct{} // closed when the saver has returned
	shutOnce  sync.Once

	checkMu     sync.Mutex // guards the two fields below
	checkCancel context.CancelFunc
	checkDone   chan struct{} // closed when the running check has returned
}

var errNoHistory = errors.New("history is unavailable")

// NewApp opens history and applies the saved proxy before wails.Run: Wails calls
// OnStartup concurrently with the frontend, whose first calls need both.
func NewApp() (*App, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	a := &App{dataDir: filepath.Join(dir, "sonar")}
	a.init()
	return a, nil
}

func (a *App) init() {
	if err := os.MkdirAll(a.dataDir, 0o755); err != nil {
		log.Printf("data dir: %v", err)
	}
	// A second instance holds the lock; the app still runs, without history.
	if h, err := history.Open(filepath.Join(a.dataDir, "history.db")); err != nil {
		log.Printf("history: %v", err)
		a.second = errors.Is(err, bolt.ErrTimeout)
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
	a.dd = doubledouble.New(filepath.Join(a.dataDir, "doubledouble-token"))
	a.findFFmpeg = convert.FindFFmpeg
	a.ffLatest, a.ffInstall = ffinstall.Latest, ffinstall.Install
	a.amazonPreview = amazon.PreviewURL
	sources := source.NewBalancer(sourceCooldown, lucida.New(), a.dd)
	if a.dd.Token() == "" { // first run: say so before any track fails
		sources.NeedCheck("doubledouble")
	}
	a.startQueue(amazonFetch(a.fetch), sources, resolve.Live()) // shim until Task 13
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// shutdown saves the queue before Close: Close cancels the downloads, and a save after it would drop them.
func (a *App) shutdown(context.Context) {
	a.shutOnce.Do(func() {
		if !a.second {
			close(a.saveStop)
			<-a.saverDone
			a.saveQueue()
		}
		a.stopCheck(5 * time.Second)
		a.CancelFFmpegInstall()
		a.dl.Close()
		if a.hist != nil {
			a.hist.Close()
		}
	})
}

// GetMetadata returns the JSON of a Spotify or Amazon track, album, playlist or artist.
func (a *App) GetMetadata(url string) (string, error) {
	var get func(context.Context, string) (any, error)
	switch {
	case spotify.IsLink(url):
		get = spotify.GetMetadata
	case amazon.IsLink(url):
		get = amazon.GetMetadata
	default:
		return "", errors.New("Unsupported link. Paste a Spotify or Amazon Music link.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	data, err := get(ctx, url)
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

type AmazonSearchRequest struct {
	Query string `json:"query"`
}

func (a *App) SearchAmazon(req AmazonSearchRequest) (*model.SearchResponse, error) {
	if req.Query == "" {
		return nil, errors.New("search query is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return amazon.SearchCatalog(ctx, req.Query)
}

var spotifyID = regexp.MustCompile(`^[A-Za-z0-9]{22}$`)

// GetPreviewURL takes a Spotify or an Amazon track ID; anything else gets "".
func (a *App) GetPreviewURL(trackID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if spotifyID.MatchString(trackID) {
		return spotify.PreviewURL(ctx, trackID)
	}
	if _, err := amazon.TrackURL(trackID); err == nil {
		return a.amazonPreview(ctx, trackID)
	}
	return "", nil
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

// LoadSettings returns an empty map when nothing has been saved yet.
func (a *App) LoadSettings() (map[string]any, error) {
	data, err := os.ReadFile(a.configPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	s := map[string]any{}
	err = json.Unmarshal(data, &s)
	if s == nil { // file held "null"
		s = map[string]any{}
	}
	return s, err
}

func (a *App) GetFetchHistory() ([]history.FetchItem, error) {
	if a.hist == nil {
		return nil, errNoHistory
	}
	return a.hist.List()
}

// GetFetchHistoryData returns the cached payload of one history item.
func (a *App) GetFetchHistoryData(id string) (string, error) {
	if a.hist == nil {
		return "", errNoHistory
	}
	return a.hist.Data(id)
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
