package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sonar/internal/convert"
	"sonar/internal/download"
	"sonar/internal/plugin"
	"sonar/internal/resolve"
	"sonar/internal/source"
	"sonar/internal/tags"
)

// errNoFFmpeg starts with the NO_FFMPEG: sentinel the frontend matches on to offer the Windows-only installer.
var errNoFFmpeg = errors.New(noFFmpegPrefixFor(goruntime.GOOS) + noFFmpegMsg)

const noFFmpegMsg = "FFmpeg is needed to convert to MP3 or M4A; set its path in Settings → Downloads"

func noFFmpegPrefixFor(goos string) string {
	if goos == "windows" {
		return "NO_FFMPEG: "
	}
	return ""
}

const sourceCooldown = 10 * time.Minute

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

// startQueue wires the download sites, the plugins and a new queue, restores queue.json, starts the
// enabled downloaders and the saver; a second instance does none of that. The Balancer starts empty, so the queue
// starts no workers of its own: syncDownloaders adds each site with its plugin's parallel count, before the
// hooks, so that adding them asks for no save. Every hook is set before Restore, any Enqueue and the first
// plugin start: none of them is read under a lock.
func (a *App) startQueue(fetch download.Fetch) {
	a.cache = resolve.OpenCache(filepath.Join(a.dataDir, "matches.json"))
	a.sources = source.NewBalancer(sourceCooldown)
	a.dl = download.NewQueue(fetch, resolve.New(resolve.PluginFinders(a.reg), a.cache).Resolve, a.sources)
	a.saveCh, a.saveStop, a.saverDone = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	a.syncDownloaders()
	a.sources.OnChange = func() { a.dl.Wake(); a.saveSoon() }
	a.dl.OnChange = a.saveSoon
	a.reg.OnChange = func() { a.syncDownloaders(); a.emitPlugins() }
	a.reg.OnStatus = a.pluginStatus
	if a.second {
		return
	}
	a.startDownloaders()

	// A corrupt file is left on disk until the next save replaces it.
	st, err := download.LoadState(a.queuePath())
	if err != nil {
		log.Printf("queue %s: %v; starting empty", a.queuePath(), err)
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

// Seams for tests.
var (
	convertFile = convert.Convert
	tagBudget   = 20 * time.Second // the longest the taggers may hold a download back, counted from their start
)

// fetch is the queue's worker for one site: the taggers look up while the site works, FFmpeg converts when asked,
// then the file gets what it lacks from the page data and what the taggers found. Tags never fail the track.
func (a *App) fetch(ctx context.Context, site string, t download.Track, ids map[string]string, o download.Options, pathNoExt string, onStatus func(string)) (string, error) {
	// Checked before the download so a file is never fetched just to be deleted.
	ff, err := a.ffmpeg(o)
	if errors.Is(err, errNoFFmpeg) {
		return "", errors.New(noFFmpegMsg) // the sentinel is for DownloadTracks; a queued item shows plain text
	}
	if err != nil {
		return "", err
	}
	ids = maps.Clone(ids) // the queue's map is never written; downloaders and taggers get the track's own id too
	if ids == nil {
		ids = map[string]string{}
	}
	ids[download.ServiceOf(t)] = t.ID
	tctx, stop := context.WithTimeout(ctx, tagBudget)
	defer stop() // ends the lookups when fetch returns
	found := a.lookUpTags(tctx, t, ids)

	f, err := a.sources.Fetch(ctx, site, source.Request{Track: t, IDs: ids, PathNoExt: pathNoExt, OnStatus: onStatus})
	if errors.Is(err, source.ErrUnavailable) || errors.Is(err, source.ErrNeedsCheck) {
		return "", fmt.Errorf("%w: %w", download.ErrSiteDown, err)
	}
	if err != nil {
		return f, err
	}
	fields := waitTags(tctx, found, onStatus)
	if o.Format != "original" {
		onStatus("Converting to " + map[string]string{"mp3-320": "MP3", "m4a-aac-320": "M4A"}[o.Format] + "…")
		if f, err = convertFile(ctx, ff, f, o.Format); err != nil {
			return f, err
		}
	}
	if ff == "" && !strings.EqualFold(filepath.Ext(f), ".flac") { // a site's MP3 or M4A needs FFmpeg for its tags
		ff = a.findFFmpeg(o.FFmpegPath)
	}
	a.writeTags(ctx, ff, f, t, fields, onStatus)
	return f, nil
}

// lookUpTags starts every enabled tagger on t and gives one channel per tagger, in order, each receiving once.
func (a *App) lookUpTags(ctx context.Context, t download.Track, ids map[string]string) []chan tags.Tags {
	var out []chan tags.Tags
	for _, tg := range a.reg.Taggers() {
		ch := make(chan tags.Tags, 1)
		out = append(out, ch)
		go func() { ch <- a.tagWith(ctx, tg, t, ids) }()
	}
	return out
}

// tagWith runs one tagger, first finding the id it needs; a tagger that fails is logged and skipped.
func (a *App) tagWith(ctx context.Context, tg *plugin.TaggerClient, t download.Track, ids map[string]string) tags.Tags {
	skip := func(err error) tags.Tags {
		if !errors.Is(err, context.Canceled) { // cancelled = fetch already returned
			log.Printf("tagger %s, track %s: %v", tg.ID, t.ID, err)
		}
		return nil
	}
	if tg.Needs != "" && ids[tg.Needs] == "" {
		id, err := a.idFor(ctx, t, ids, tg.Needs)
		if err != nil {
			return skip(err)
		}
		ids = maps.Clone(ids)
		ids[tg.Needs] = id
	}
	f, err := tg.Tag(ctx, plugin.TagParams{Track: t, IDs: ids})
	if err != nil {
		return skip(err)
	}
	return f
}

// waitTags collects the taggers' answers until ctx is done, saying so while it waits.
func waitTags(ctx context.Context, found []chan tags.Tags, onStatus func(string)) []tags.Tags {
	out := make([]tags.Tags, len(found))
	waiting := false
	for i, ch := range found {
		select {
		case out[i] = <-ch:
			continue
		default:
		}
		if ctx.Err() != nil {
			continue
		}
		if !waiting {
			onStatus("Looking up tags…")
			waiting = true
		}
		select {
		case out[i] = <-ch:
		case <-ctx.Done():
		}
	}
	return out
}

// idFor is t's id on service: its own, one already found, else the first track with t's ISRC, which comes from
// t or from its own source's Tracks.
func (a *App) idFor(ctx context.Context, t download.Track, ids map[string]string, service string) (string, error) {
	own := download.ServiceOf(t)
	if own == service {
		return t.ID, nil
	}
	if id := ids[service]; id != "" {
		return id, nil
	}
	isrc := t.ISRC
	if isrc == "" {
		src, err := a.reg.Source(own)
		if err != nil {
			return "", err
		}
		infos, err := src.Tracks(ctx, []string{t.ID})
		if err != nil {
			return "", err
		}
		if len(infos) == 0 || infos[0].ISRC == "" {
			return "", fmt.Errorf("no ISRC for %s track %s", own, t.ID)
		}
		isrc = infos[0].ISRC
	}
	dst, err := a.reg.Source(service)
	if err != nil {
		return "", err
	}
	found, err := dst.ISRC(ctx, isrc)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", fmt.Errorf("no %s track with ISRC %s", service, isrc)
	}
	return found[0], nil
}

// writeTags fills what the file at path lacks from t, lets the taggers' fields overwrite, and writes only what changed.
// A tagger's "cover" is a URL whose image always replaces the cover; else t's image goes into a file without one.
// Nothing here fails the track: a missing FFmpeg becomes the item's message, anything else is logged.
func (a *App) writeTags(ctx context.Context, ff, path string, t download.Track, found []tags.Tags, onStatus func(string)) {
	coverURL := ""
	taggers := make([]tags.Tags, 0, len(found))
	for _, tg := range found {
		if u := tg["cover"]; u != "" {
			coverURL = u // the last tagger's wins, as for every field
		}
		tg = maps.Clone(tg)
		delete(tg, "cover")
		taggers = append(taggers, tg)
	}
	err := func() error {
		existing, hasCover, err := tags.Read(ctx, ff, path)
		if err != nil {
			return err
		}
		out, _ := tags.Merge(existing, tags.Base(t), taggers)
		diff := tags.Tags{}
		for k, v := range out {
			if existing[k] != v {
				diff[k] = v
			}
		}
		var cover []byte
		switch {
		case coverURL != "":
			cover = fetchCover(ctx, coverURL)
		case !hasCover && t.Image != "":
			cover = fetchCover(ctx, t.Image)
		}
		if len(diff) == 0 && cover == nil {
			return nil
		}
		err = tags.Write(ctx, ff, path, diff, cover)
		if err != nil && cover != nil && len(diff) > 0 { // e.g. a cover type the file cannot take: keep the text
			log.Printf("tags %s: %v; writing them without the cover", path, err)
			err = tags.Write(ctx, ff, path, diff, nil)
		}
		return err
	}()
	switch {
	case errors.Is(err, tags.ErrNoFFmpeg):
		onStatus(err.Error())
	case err != nil && !errors.Is(err, context.Canceled):
		log.Printf("tags %s: %v", path, err)
	}
}

func fetchCover(ctx context.Context, url string) []byte {
	b, err := tags.FetchCover(ctx, url)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("cover %s: %v", url, err)
	}
	return b
}

// DownloadTracks validates the whole request before it queues anything; tracks without an id are skipped,
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
	if _, err := a.ffmpeg(o); err != nil {
		return err
	}
	if o.Source == "" {
		o.Source = source.Auto
	}
	if o.Source != source.Auto {
		if !a.sources.Has(o.Source) {
			return fmt.Errorf("unknown download source %q", o.Source)
		}
		for _, s := range a.sources.Status() {
			if s.Name == o.Source && s.NeedsAction { // only the user can bring it back: say so now, not per track
				return errors.New(s.Reason)
			}
		}
	}
	if o.Dir == "" {
		o.Dir = a.DefaultDownloadDir()
	}
	services := map[string]bool{} // service → whether a source for it is on; an off one's tracks would never be placed
	for _, e := range a.reg.List() {
		if e.Err == "" && e.M.Kind == plugin.KindSource && e.M.Source != nil {
			services[e.M.Source.Service] = services[e.M.Source.Service] || e.Enabled
		}
	}
	tracks := make([]download.Track, 0, len(req.Tracks))
	var skipped error
	for _, t := range req.Tracks {
		if t.ID == "" { // one odd track must not stop a whole playlist
			skipped = errors.New("a track has no id")
			log.Printf("download: skipping %q: %v", t.Title, skipped)
			continue
		}
		if svc := download.ServiceOf(t); !services[svc] {
			if err := a.builtinMissing(); err != nil {
				return err
			}
			if _, off := services[svc]; off {
				return errors.New(a.serviceLabel(svc) + " is switched off")
			}
			return fmt.Errorf("unknown service %q", svc)
		}
		if k := req.Collection.Kind; (k == "album" || k == "artist") && t.AlbumID != "" {
			t.Group = "album:" + t.AlbumID // each release is matched as a whole
		}
		tracks = append(tracks, t)
	}
	if len(tracks) == 0 && skipped != nil {
		return skipped
	}
	a.dl.Enqueue(req.Collection, o, tracks)
	return nil
}

func (a *App) GetDownloadState() DownloadState {
	sites := a.sources.Status()
	return DownloadState{Items: a.explainOff(a.dl.Snapshot(), sites), Sites: sites, Held: a.dl.Held()}
}

// explainOff gives a queued track fixed to a switched-off site the reason it waits: sites leaves such a site out.
func (a *App) explainOff(items []download.Item, sites []source.Status) []download.Item {
	var fixed map[string]string // item id → its fixed source, read once and only when needed
	for i := range items {
		it := &items[i]
		if it.Status != download.Queued || it.Message != "" {
			continue
		}
		if fixed == nil {
			fixed = map[string]string{}
			for _, s := range a.dl.Pending() {
				fixed[s.Item.ID] = s.Options.Source
			}
		}
		name := fixed[it.ID]
		if name == "" || name == source.Auto || !a.sources.Has(name) || slices.ContainsFunc(sites, func(s source.Status) bool { return s.Name == name }) {
			continue
		}
		label := name
		if e, ok := a.reg.Get(name); ok {
			label = e.M.Name
		}
		it.Message = label + " is switched off"
	}
	return items
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

func (a *App) item(id string) (download.Item, bool) {
	for _, it := range a.dl.Snapshot() {
		if it.ID == id {
			return it, true
		}
	}
	return download.Item{}, false
}

// ChooseCandidate downloads targetID, an id on the item's ReviewService, for a track in review
// and remembers the choice for next time.
func (a *App) ChooseCandidate(id, targetID string) error {
	it, ok := a.item(id)
	if err := a.dl.Choose(id, targetID); err != nil {
		return err
	}
	if ok {
		a.cache.Put(download.ServiceOf(it.Track), it.ReviewService, it.Track.ID, targetID, "manual", "")
	}
	return nil
}

// UseLink is ChooseCandidate with the track a pasted link points at; the link must be on the item's ReviewService.
func (a *App) UseLink(id, link string) error {
	it, ok := a.item(id)
	if !ok || it.Status != download.Review {
		return errors.New("track is not waiting for a choice")
	}
	bad := fmt.Errorf("paste a link to a track on %s", a.serviceLabel(it.ReviewService))
	s, ok := a.reg.SourceForLink(link)
	if !ok || s.Service != it.ReviewService {
		return bad
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	defer cancel()
	l, err := s.ParseLink(ctx, link)
	switch {
	case errors.Is(err, plugin.ErrCannot):
		return bad
	case err != nil:
		return err
	case l.Kind != "track" || l.ID == "":
		return bad
	}
	return a.ChooseCandidate(id, l.ID)
}

func (a *App) SkipReview(id string) { a.dl.Skip(id) }

// GuardWait is "Wait" on an asking site: it pauses for minutes, one of its guard's waits (0 means its default),
// then resumes by itself.
func (a *App) GuardWait(site string, minutes int) error {
	e, ok := a.reg.Get(site)
	if !ok || !a.sources.Has(site) {
		return fmt.Errorf("unknown download source %q", site)
	}
	g := guardOf(e)
	if minutes == 0 {
		minutes = g.Wait
	}
	if !slices.Contains(g.Waits, minutes) {
		return guardWaitErr(g.Waits)
	}
	a.sources.GuardWait(site, time.Duration(minutes)*time.Minute)
	return nil
}

// GuardContinue is "Continue anyway", and also "Resume now" during a pause.
func (a *App) GuardContinue(site string) { a.sources.GuardContinue(site) }

func (a *App) CancelDownload(id string)         { a.dl.Cancel(id) }
func (a *App) CancelAllDownloads()              { a.dl.CancelAll() }
func (a *App) RetryDownload(id string)          { a.dl.Retry(id) }
func (a *App) RetryFailedDownloads()            { a.dl.RetryFailed() }
func (a *App) ClearFinishedDownloads()          { a.dl.ClearFinished() }
func (a *App) GetSourceStatus() []source.Status { return a.sources.Status() }

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
