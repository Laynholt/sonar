package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sonar/internal/amazon"
	"sonar/internal/convert"
	"sonar/internal/doubledouble"
	"sonar/internal/download"
	"sonar/internal/resolve"
	"sonar/internal/source"
)

// errNoFFmpeg starts with the NO_FFMPEG: sentinel the frontend matches on.
var errNoFFmpeg = errors.New("NO_FFMPEG: FFmpeg is needed to convert to MP3 or M4A; set its path in Settings → Downloads")

const (
	sourceCooldown = 10 * time.Minute
	checkTimeout   = 10 * time.Minute
	checkTrackURL  = "https://music.amazon.com/tracks/B00A3SGXAA"
)

type DownloadRequest struct {
	Collection download.Collection `json:"collection"`
	Options    download.Options    `json:"options"`
	Tracks     []download.Track    `json:"tracks"`
}

// DownloadState is everything the Downloads page shows. Held is only the restored queue's «Позже» hold.
type DownloadState struct {
	Items []download.Item `json:"items"`
	Sites []source.Status `json:"sites"`
	Held  bool            `json:"held"`
}

// PendingQueue is what the resume dialog shows; Tracks counts the entries still to match, review or download.
type PendingQueue struct {
	Tracks      int      `json:"tracks"`
	Review      int      `json:"review"`
	Collections []string `json:"collections"`
}

func (a *App) queuePath() string { return filepath.Join(a.dataDir, "queue.json") }

// startQueue wires sources and a new queue, restores queue.json and starts the saver; a second instance does neither.
// Both OnChange hooks are set before Restore and before any Enqueue: neither is read under a lock.
func (a *App) startQueue(fetch download.Fetch, sources *source.Balancer, finder resolve.Finder) {
	a.cache = resolve.OpenCache(filepath.Join(a.dataDir, "matches.json"))
	a.sources = sources
	a.dl = download.NewQueue(fetch, resolve.New(finder, a.cache).Resolve, sources)
	a.saveCh, a.saveStop, a.saverDone = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	a.sources.OnChange = func() { a.dl.Wake(); a.saveSoon() }
	a.dl.OnChange = a.saveSoon
	if a.second {
		return
	}

	// A corrupt file is left on disk until the next save replaces it.
	st, err := download.LoadState(a.queuePath())
	if err != nil {
		log.Printf("queue %s: %v; starting empty", a.queuePath(), err)
	}
	if n := len(st.Items); n > 0 { // the restored tracks keep the guard limit they were queued with
		a.sources.SetGuard(st.Items[n-1].Options.GuardLimit)
	}
	a.dl.Restore(st.Items)
	pauses := map[string]time.Time{}
	for name, ms := range st.Pauses {
		pauses[name] = time.UnixMilli(ms)
	}
	a.sources.RestoreGuardPauses(pauses)
	go a.saver()
}

// saveSoon asks the saver to write queue.json; requests made while one is pending fold into it.
func (a *App) saveSoon() {
	select {
	case a.saveCh <- struct{}{}:
	default:
	}
}

func (a *App) saver() {
	defer close(a.saverDone)
	for {
		select {
		case <-a.saveCh:
			a.saveQueue()
		case <-a.saveStop:
			return
		}
	}
}

func (a *App) saveQueue() {
	pauses := map[string]int64{}
	for name, until := range a.sources.GuardPauses() {
		pauses[name] = until.UnixMilli()
	}
	st := download.State{Version: 1, Items: a.dl.Pending(), Pauses: pauses}
	if err := download.SaveState(a.queuePath(), st); err != nil {
		log.Printf("save queue: %v", err)
	}
}

// ffmpeg returns the FFmpeg that o's format needs, "" for none, or errNoFFmpeg when it is missing.
func (a *App) ffmpeg(o download.Options) (string, error) {
	if o.Format == "original" {
		return "", nil
	}
	if ff := a.findFFmpeg(o.FFmpegPath); ff != "" {
		return ff, nil
	}
	return "", errNoFFmpeg
}

// fetch is the queue's worker for one site: the site delivers FLAC, FFmpeg converts it when asked.
func (a *App) fetch(ctx context.Context, site, id string, o download.Options, pathNoExt string, onStatus func(string)) (string, error) {
	url, err := amazon.TrackURL(id)
	if err != nil {
		return "", err
	}
	// Checked before the download so a FLAC is never fetched just to be deleted.
	ff, err := a.ffmpeg(o)
	if err != nil {
		return "", err
	}
	f, err := a.sources.Fetch(ctx, site, url, pathNoExt, onStatus)
	if errors.Is(err, source.ErrUnavailable) || errors.Is(err, source.ErrNeedsCheck) {
		return "", fmt.Errorf("%w: %w", download.ErrSiteDown, err)
	}
	if err != nil || o.Format == "original" {
		return f, err
	}
	onStatus("Converting to " + map[string]string{"mp3-320": "MP3", "m4a-aac-320": "M4A"}[o.Format] + "…")
	return convert.Convert(ctx, ff, f, o.Format)
}

// DownloadTracks validates the whole request before it queues anything; tracks with a bad Spotify ID are skipped,
// and only a request left with none of its tracks fails.
func (a *App) DownloadTracks(req DownloadRequest) error {
	o := req.Options
	if o.Format == "" {
		o.Format = "original"
	}
	if !slices.Contains(convert.Formats, o.Format) {
		return fmt.Errorf("unknown download format %q", o.Format)
	}
	// Up front: in fetch this error would fail the track on every site in turn.
	_, err := a.ffmpeg(o)
	if err != nil {
		return err
	}
	if o.GuardLimit < 0 {
		return errors.New("guard limit must be 0 or more")
	}
	if o.GuardWait, err = guardMinutes(o.GuardWait); err != nil {
		return err
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
	tracks := make([]download.Track, 0, len(req.Tracks))
	var skipped error
	for _, t := range req.Tracks {
		// The queue matches every track without an Amazon ID, so only these two services may pass.
		switch t.Service {
		case "", "amazon":
			if _, err := amazon.TrackURL(t.ID); err != nil {
				return err
			}
		case "spotify":
			if !spotifyID.MatchString(t.ID) { // one odd track must not stop a whole playlist
				skipped = fmt.Errorf("not a Spotify track ID: %q", t.ID)
				log.Printf("download: skipping %v", skipped)
				continue
			}
			if k := req.Collection.Kind; (k == "album" || k == "artist") && t.AlbumID != "" {
				t.Group = "album:" + t.AlbumID // each release is matched as a whole
			}
		default:
			return fmt.Errorf("unknown service %q", t.Service)
		}
		tracks = append(tracks, t)
	}
	if len(tracks) == 0 && skipped != nil {
		return skipped
	}
	a.sources.SetGuard(o.GuardLimit)
	a.dl.Enqueue(req.Collection, o, tracks)
	return nil
}

func (a *App) GetDownloadState() DownloadState {
	return DownloadState{Items: a.dl.Snapshot(), Sites: a.sources.Status(), Held: a.dl.Held()}
}

// GetPendingQueue is zero unless the restored queue is held, so the resume dialog shows only after a restart.
func (a *App) GetPendingQueue() PendingQueue {
	p := PendingQueue{Collections: []string{}} // [] in JSON, never null
	if !a.dl.Held() {
		return p
	}
	for _, s := range a.dl.Pending() {
		switch s.Item.Status {
		case download.Failed:
			continue // kept for Retry, but there is nothing to resume
		case download.Review:
			p.Review++
		}
		p.Tracks++
		if !slices.Contains(p.Collections, s.Collection.Name) {
			p.Collections = append(p.Collections, s.Collection.Name)
		}
	}
	return p
}

func (a *App) ResumeQueue() { a.dl.Resume() }

// ClearQueue drops every unfinished entry, and the hold with them: nothing is left to resume.
func (a *App) ClearQueue() {
	a.dl.Clear()
	a.dl.Resume()
}

// ChooseCandidate downloads amazonID for a track in review and remembers the choice for next time.
func (a *App) ChooseCandidate(id, amazonID string) error {
	if _, err := amazon.TrackURL(amazonID); err != nil {
		return err
	}
	if err := a.dl.Choose(id, amazonID); err != nil {
		return err
	}
	a.cache.Put(id, amazonID, "manual")
	return nil
}

// UseAmazonLink is ChooseCandidate with the track a pasted Amazon Music link points at.
func (a *App) UseAmazonLink(id, link string) error {
	l, err := amazon.ParseLink(link)
	if err != nil || l.Kind != "track" {
		return errors.New("paste a link to an Amazon Music track")
	}
	return a.ChooseCandidate(id, l.ID)
}

func (a *App) SkipReview(id string) { a.dl.Skip(id) }

// GuardWait is "Wait" on an asking site: it pauses for minutes (0 means 30), then resumes by itself.
func (a *App) GuardWait(site string, minutes int) error {
	minutes, err := guardMinutes(minutes)
	if err != nil {
		return err
	}
	a.sources.GuardWait(site, time.Duration(minutes)*time.Minute)
	return nil
}

// guardMinutes checks a guard wait in minutes; 0 means the default 30.
func guardMinutes(m int) (int, error) {
	switch m {
	case 0:
		return 30, nil
	case 15, 30, 60, 120:
		return m, nil
	}
	return 0, errors.New("guard wait must be 15, 30, 60 or 120 minutes")
}

// GuardContinue is "Continue anyway", and also "Resume now" during a pause.
func (a *App) GuardContinue(site string) { a.sources.GuardContinue(site) }

func (a *App) CancelDownload(id string)         { a.dl.Cancel(id) }
func (a *App) CancelAllDownloads()              { a.dl.CancelAll() }
func (a *App) RetryDownload(id string)          { a.dl.Retry(id) }
func (a *App) RetryFailedDownloads()            { a.dl.RetryFailed() }
func (a *App) ClearFinishedDownloads()          { a.dl.ClearFinished() }
func (a *App) GetSourceStatus() []source.Status { return a.sources.Status() }

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
