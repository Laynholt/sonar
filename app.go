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
	"sync"
	"sync/atomic"

	bolt "go.etcd.io/bbolt"

	"sonar/internal/convert"
	"sonar/internal/download"
	"sonar/internal/ffinstall"
	"sonar/internal/history"
	"sonar/internal/httpx"
	"sonar/internal/plugin"
	"sonar/internal/resolve"
	"sonar/internal/source"
)

type App struct {
	ctx     context.Context
	uiCtx   atomic.Pointer[context.Context] // ctx for EventsEmit, which other goroutines read; nil before startup
	dataDir string                          // holds history.db, config.json, queue.json, matches.json and plugins.json
	hist    *history.DB
	second  bool // another instance holds history.db, and owns queue.json: this one neither restores nor saves it

	reg        *plugin.Registry
	pluginsDir string     // the user's plugins folder
	syncMu     sync.Mutex // serialises syncDownloaders
	sources    *source.Balancer
	dl         *download.Queue
	cache      *resolve.Cache             // matches.json, shared with the resolver
	findFFmpeg func(custom string) string // a field so tests can fake it

	zipMu      sync.Mutex // guards the two fields below
	installZip string     // the zip an install that answered "exists" came from, for its retry
	importZip  string     // the same for an import that answered a conflict

	ffLatest  func(context.Context, *http.Client, string) (ffinstall.Asset, ffinstall.Asset, error) // fields so tests can fake them
	ffInstall func(context.Context, *http.Client, ffinstall.Asset, ffinstall.Asset, string, func(ffinstall.Progress)) (string, error)
	ffMu      sync.Mutex // guards the two fields below
	ffState   FFmpegInstall
	ffCancel  context.CancelFunc // non-nil while an install runs

	saveCh    chan struct{} // cap 1: a pending request to save queue.json
	saveStop  chan struct{} // closed to stop the saver
	saverDone chan struct{} // closed when the saver has returned
	shutOnce  sync.Once
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
	a.findFFmpeg = convert.FindFFmpeg
	a.ffLatest, a.ffInstall = ffinstall.Latest, ffinstall.Install
	builtin := ""
	if exe := convert.ExeDir(); exe != "" {
		builtin = filepath.Join(exe, "plugins")
	}
	a.openPlugins(builtin, userPluginsDir(a.dataDir), plugin.ParseSums(plugin.BuiltinSums))
	a.migrateAtStartup()
	a.startQueue(a.fetch)
}

// migrateAtStartup runs migrate before the queue restores. Only the instance that owns queue.json owns the old
// files, and a migration error is logged, never fatal: the next launch retries.
func (a *App) migrateAtStartup() {
	if a.second {
		return
	}
	if err := a.migrate(); err != nil {
		log.Printf("migrate: %v", err)
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.uiCtx.Store(&ctx)
}

// shutdown saves the queue before Close: Close cancels the downloads, and a save after it would drop them.
// The plugins stop last, ending a running action such as a browser check.
func (a *App) shutdown(context.Context) {
	a.shutOnce.Do(func() {
		if !a.second {
			close(a.saveStop)
			<-a.saverDone
			a.saveQueue()
		}
		a.CancelFFmpegInstall()
		a.dl.Close()
		a.reg.Close()
		if a.hist != nil {
			a.hist.Close()
		}
	})
}

func (a *App) configPath() string { return filepath.Join(a.dataDir, "config.json") }

// SaveSettings validates the proxy first, so a bad value writes nothing. A new proxy reaches the running plugins.
func (a *App) SaveSettings(s map[string]any) error {
	proxy, _ := s["proxy"].(string)
	prev := httpx.ManualProxy()
	if err := httpx.SetManualProxy(proxy); err != nil {
		return err
	}
	if a.reg != nil && httpx.ManualProxy() != prev {
		defer a.reg.NotifySettings() // after the write, so the note carries the saved settings
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
