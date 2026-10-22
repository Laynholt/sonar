package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sonar/internal/amazon"
	"sonar/internal/convert"
	"sonar/internal/doubledouble"
	"sonar/internal/download"
	"sonar/internal/source"
)

var errNoFFmpeg = errors.New("FFmpeg is needed to convert to MP3 or M4A; set its path in Settings → Downloads")

const (
	sourceCooldown  = 10 * time.Minute
	downloadWorkers = 2 // both sites rip one track per request; don't hammer them
	checkTimeout    = 10 * time.Minute
	checkTrackURL   = "https://music.amazon.com/tracks/B00A3SGXAA"
	guardWait       = 30 * time.Minute // how long "Wait" pauses a site
)

// DownloadGuard is what the rate-guard dialog shows. PausedUntil is unix ms, 0 for none.
type DownloadGuard struct {
	Site        string `json:"site"`
	Count       int    `json:"count"`
	Asking      bool   `json:"asking"`
	PausedUntil int64  `json:"paused_until"`
}

type DownloadRequest struct {
	Collection download.Collection `json:"collection"`
	Options    download.Options    `json:"options"`
	Tracks     []download.Track    `json:"tracks"`
}

// unwiredResolve fails every Spotify track until the matcher is wired in.
func unwiredResolve(_ context.Context, tracks []download.Track) []download.Resolution {
	out := make([]download.Resolution, len(tracks))
	for i := range out {
		out[i].Err = errors.New("matching is not wired yet")
	}
	return out
}

// fetch is the queue's worker: the site delivers FLAC, FFmpeg converts it when asked.
func (a *App) fetch(ctx context.Context, id string, o download.Options, pathNoExt string, onStatus func(string)) (string, error) {
	url, err := amazon.TrackURL(id)
	if err != nil {
		return "", err
	}
	ff := ""
	if o.Format != "original" {
		// Checked before the download so a FLAC is never fetched just to be deleted.
		if ff = a.findFFmpeg(o.FFmpegPath); ff == "" {
			return "", errNoFFmpeg
		}
	}
	site := o.Source
	if site == source.Auto { // shortcut: always the first site until Task 6 gives each site its own worker
		site = a.sources.Names()[0]
	}
	f, err := a.sources.Fetch(ctx, site, url, pathNoExt, onStatus)
	if err != nil || o.Format == "original" {
		return f, err
	}
	onStatus("Converting to " + map[string]string{"mp3-320": "MP3", "m4a-aac-320": "M4A"}[o.Format] + "…")
	return convert.Convert(ctx, ff, f, o.Format)
}

// DownloadTracks validates the whole request before it queues anything.
func (a *App) DownloadTracks(req DownloadRequest) error {
	o := req.Options
	if o.Format == "" {
		o.Format = "original"
	}
	if !slices.Contains(convert.Formats, o.Format) {
		return fmt.Errorf("unknown download format %q", o.Format)
	}
	if o.Format != "original" && a.findFFmpeg(o.FFmpegPath) == "" {
		return errNoFFmpeg
	}
	if o.GuardLimit < 0 {
		return errors.New("guard limit must be 0 or more")
	}
	if o.Source == "" {
		o.Source = source.Auto
	}
	if o.Source != source.Auto && !a.sources.Has(o.Source) {
		return fmt.Errorf("unknown download source %q", o.Source)
	}
	if o.Source == "doubledouble" && a.dd.Token() == "" {
		return errors.New("pass the doubledouble check in Settings → Downloads first")
	}
	if o.Dir == "" {
		o.Dir = a.DefaultDownloadDir()
	}
	for _, t := range req.Tracks {
		if _, err := amazon.TrackURL(t.ID); err != nil {
			return fmt.Errorf("only Amazon Music tracks can be downloaded for now: %s", t.ID)
		}
	}
	a.sources.SetGuard(o.GuardLimit, guardWait)
	a.dl.Enqueue(req.Collection, o, req.Tracks)
	return nil
}

// syncGuard is the balancer's OnChange: it holds the whole queue while any site's guard asks or pauses.
// shortcut: one site's guard stops both sites; Task 6/8 gate each site on its own.
func (a *App) syncGuard() {
	a.guardMu.Lock() // so concurrent calls apply the latest state last
	defer a.guardMu.Unlock()
	if a.GetDownloadGuard() != (DownloadGuard{}) {
		a.dl.Hold(time.Time{})
	} else {
		a.dl.Resume()
	}
}

// GetDownloadGuard is the first asking site, else the first guard-paused one, or zero for none.
func (a *App) GetDownloadGuard() DownloadGuard {
	st := a.sources.Status()
	i := slices.IndexFunc(st, func(s source.Status) bool { return s.Asking })
	if i < 0 {
		i = slices.IndexFunc(st, func(s source.Status) bool { return s.PausedUntil != 0 })
	}
	if i < 0 {
		return DownloadGuard{}
	}
	s := st[i]
	return DownloadGuard{Site: s.Name, Count: s.Count, Asking: s.Asking, PausedUntil: s.PausedUntil}
}

// GuardWait is "OK, I'll wait": that site pauses for guardWait, then resumes by itself.
func (a *App) GuardWait() { a.sources.GuardWait(a.GetDownloadGuard().Site) }

// GuardContinue is "Continue anyway", and also "Resume now" during a pause.
func (a *App) GuardContinue() { a.sources.GuardContinue(a.GetDownloadGuard().Site) }

func (a *App) GetDownloadQueue() []download.Item { return a.dl.Snapshot() }
func (a *App) CancelDownload(id string)          { a.dl.Cancel(id) }
func (a *App) CancelAllDownloads()               { a.dl.CancelAll() }
func (a *App) RetryDownload(id string)           { a.dl.Retry(id) }
func (a *App) RetryFailedDownloads()             { a.dl.RetryFailed() }
func (a *App) ClearFinishedDownloads()           { a.dl.ClearFinished() }
func (a *App) GetSourceStatus() []source.Status  { return a.sources.Status() }

// PassDoubleDoubleCheck blocks until the user passes the check in the browser window or it ends.
func (a *App) PassDoubleDoubleCheck() error {
	browser := doubledouble.FindBrowser()
	if browser == "" {
		return errors.New("Chrome or Edge is needed for the doubledouble check")
	}
	err := a.runCheck(func(ctx context.Context) error {
		return a.dd.Check(ctx, browser, filepath.Join(a.dataDir, "doubledouble-browser"), checkTrackURL)
	})
	if err != nil {
		return err
	}
	a.sources.Reset("doubledouble")
	return nil
}

// runCheck runs fn under the check timeout. The ctx is not Wails' (it is never cancelled),
// so stopCheck can end the check, and with it the browser window, on quit.
func (a *App) runCheck(fn func(ctx context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	done := make(chan struct{})
	a.checkMu.Lock()
	if a.checkCancel != nil {
		a.checkMu.Unlock()
		return errors.New("a doubledouble check is already open")
	}
	a.checkCancel, a.checkDone = cancel, done
	a.checkMu.Unlock()
	defer func() {
		a.checkMu.Lock()
		a.checkCancel, a.checkDone = nil, nil
		a.checkMu.Unlock()
		close(done)
	}()
	err := fn(ctx)
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("the doubledouble check timed out")
	}
	return err
}

// stopCheck cancels a running check and waits up to wait for it to return.
func (a *App) stopCheck(wait time.Duration) {
	a.checkMu.Lock()
	cancel, done := a.checkCancel, a.checkDone
	a.checkMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	select {
	case <-done:
	case <-time.After(wait):
	}
}

// DefaultDownloadDir is <home>/Music/sonar, or <dataDir>/downloads when the home dir is unknown.
func (a *App) DefaultDownloadDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, "Music", "sonar")
	}
	return filepath.Join(a.dataDir, "downloads")
}

// SelectDownloadFolder returns "" when the user cancels.
func (a *App) SelectDownloadFolder(current string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{DefaultDirectory: existingDir(current)})
}

// existingDir returns dir or its nearest existing parent, or "": the dialog fails on a missing folder,
// and the default one does not exist before the first download.
func existingDir(dir string) string {
	for dir != "" {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

func (a *App) FindFFmpeg(custom string) string { return a.findFFmpeg(custom) }

// SelectFFmpeg returns "" when the user cancels.
func (a *App) SelectFFmpeg() (string, error) {
	var filters []runtime.FileFilter
	if runtime.Environment(a.ctx).Platform == "windows" {
		filters = []runtime.FileFilter{{DisplayName: "ffmpeg.exe", Pattern: "ffmpeg.exe"}}
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Filters: filters})
}
