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
)

type DownloadRequest struct {
	Collection download.Collection `json:"collection"`
	Options    download.Options    `json:"options"`
	Tracks     []download.Track    `json:"tracks"`
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
	f, err := a.sources.Download(ctx, o.Source, url, pathNoExt, onStatus)
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
	if o.Source == "" {
		o.Source = source.Auto
	}
	if o.Source != source.Auto && !a.sources.Has(o.Source) {
		return fmt.Errorf("unknown download source %q", o.Source)
	}
	if o.Dir == "" {
		o.Dir = a.DefaultDownloadDir()
	}
	for _, t := range req.Tracks {
		if _, err := amazon.TrackURL(t.ID); err != nil {
			return fmt.Errorf("only Amazon Music tracks can be downloaded for now: %s", t.ID)
		}
	}
	a.dl.Enqueue(req.Collection, o, req.Tracks)
	return nil
}

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
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, checkTimeout)
	defer cancel()
	if err := a.dd.Check(ctx, browser, filepath.Join(a.dataDir, "doubledouble-browser"), checkTrackURL); err != nil {
		return err
	}
	a.sources.Reset("doubledouble")
	return nil
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
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{DefaultDirectory: current})
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

// PreviewDownloadPath shows where a sample track would land with these options.
func (a *App) PreviewDownloadPath(o download.Options) string {
	if o.Format == "" {
		o.Format = "original"
	}
	if o.Dir == "" {
		o.Dir = a.DefaultDownloadDir()
	}
	t := download.Track{Title: "Global Warming", Artist: "Pitbull", Album: "Global Warming", AlbumArtist: "Pitbull",
		ReleaseDate: "2012-11-16", TrackNumber: 1, DiscNumber: 1, Position: 1}
	c := download.Collection{Kind: "album", ID: "B00A3SGWYC", Name: "Global Warming"}
	return download.PathNoExt(t, c, o) + convert.Ext(o.Format)
}
