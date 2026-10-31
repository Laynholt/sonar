package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sonar/internal/httpx"
	"sonar/internal/model"
	"sonar/internal/plugin"
	"sonar/internal/source"
)

// actionTimeout bounds a plugin's settings action, e.g. a browser check the user has to pass. A var for tests.
var actionTimeout = 10 * time.Minute

// sourceTimeout bounds a source plugin's search, preview or link lookup, metadataTimeout its metadata fetch. Vars for tests.
var (
	sourceTimeout   = 30 * time.Second
	metadataTimeout = 120 * time.Second
)

// The guard fields a downloader with a guard gets on top of its own settings, stored with them.
const (
	guardLimitKey = "$guardLimit"
	guardWaitKey  = "$guardWait"
)

// SourceView is an enabled source as the search box and the service picker see it.
type SourceView struct {
	Service string   `json:"service"`
	Label   string   `json:"label"`
	Icon    string   `json:"icon"`
	Can     []string `json:"can"`
}

// PluginView is one plugin as the plugins page shows it.
type PluginView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	Dir         string `json:"dir"`
	Error       string `json:"error"` // the manifest error, else the last crash

	Builtin  bool     `json:"builtin"`
	Enabled  bool     `json:"enabled"`
	Trusted  bool     `json:"trusted"`
	Shadowed []string `json:"shadowed"`

	Fields     []plugin.Field `json:"fields"` // with the two guard fields for a downloader with a guard
	Values     map[string]any `json:"values"`
	Dependents []string       `json:"dependents"` // ids of enabled plugins that need this source
	Blocked    string         `json:"blocked"`    // "needs source Amazon Music" when a source it needs is off or missing
}

// userPluginsDir is where the user's plugins go: %LOCALAPPDATA%\Sonar\plugins, else <dataDir>/plugins.
func userPluginsDir(dataDir string) string {
	if d := os.Getenv("LOCALAPPDATA"); d != "" {
		return filepath.Join(d, "Sonar", "plugins")
	}
	return filepath.Join(dataDir, "plugins")
}

func (a *App) openPlugins(builtinDir, userDir string, sums map[string]string) {
	a.pluginsDir = userDir
	a.reg = plugin.OpenRegistry(a.dataDir, builtinDir, userDir, sums, a.pluginInit, log.Printf)
}

// pluginInit is what the app adds to a plugin's initialize and $/settings: the language and the proxy.
func (a *App) pluginInit(string) plugin.InitParams {
	s, _ := a.LoadSettings()
	lang, _ := s["language"].(string)
	if lang == "" || lang == "auto" {
		lang = "en"
	}
	return plugin.InitParams{Lang: lang, Proxy: httpx.ManualProxy(), EffectiveProxy: httpx.ProxyFor("https://example.com")}
}

// emitPlugins tells the UI that a plugin changed. It never blocks: plugin status notes arrive on a host's reader.
func (a *App) emitPlugins() {
	if ctx := a.uiCtx.Load(); ctx != nil { // EventsEmit is fatal before startup
		go runtime.EventsEmit(*ctx, "plugins:changed")
	}
}

// pluginStatus routes a plugin's $/status note to its download site, if it is one.
func (a *App) pluginStatus(id string, st plugin.PluginStatus) {
	if a.sources.Has(id) {
		switch {
		case st.NeedsAction != nil:
			a.sources.NeedAction(id, id+": "+st.NeedsAction.Message, st.NeedsAction.Action)
		case st.Available:
			a.sources.Reset(id)
		}
	}
	a.emitPlugins()
}

// toInt reads a whole number from a setting, which JSON makes a float64.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

// guardOf is a downloader's guard with the user's two guard fields applied; no guard is the zero Guard (off).
func guardOf(e plugin.Entry) source.Guard {
	if e.M.Downloader == nil || e.M.Downloader.Guard == nil {
		return source.Guard{}
	}
	g := e.M.Downloader.Guard
	out := source.Guard{Limit: g.Limit, Waits: slices.Clone(g.Waits), Wait: g.Wait}
	if n, ok := toInt(e.Values[guardLimitKey]); ok {
		out.Limit = n
	}
	if n, ok := toInt(e.Values[guardWaitKey]); ok {
		out.Wait = n
	}
	return out
}

// orList joins items as "a, b or c".
func orList(items []string) string {
	if len(items) < 2 {
		return strings.Join(items, "")
	}
	return strings.Join(items[:len(items)-1], ", ") + " or " + items[len(items)-1]
}

func guardWaitErr(waits []int) error {
	s := make([]string, len(waits))
	for i, w := range waits {
		s[i] = strconv.Itoa(w)
	}
	return fmt.Errorf("guard wait must be %s minutes", orList(s))
}

// syncDownloaders makes the Balancer and the queue match the downloader plugins: new ones are added with workers,
// known ones get their switch and guard, and sites whose plugin is gone are switched off.
func (a *App) syncDownloaders() {
	a.syncMu.Lock()
	defer a.syncMu.Unlock()
	seen := map[string]bool{}
	for _, d := range a.reg.Downloaders() {
		e, ok := a.reg.Get(d.ID)
		if !ok {
			continue
		}
		seen[d.ID] = true
		a.sources.Add(d, d.Info.Accepts, guardOf(e))
		a.sources.SetEnabled(d.ID, e.Enabled)
		a.dl.AddSite(d.ID, d.Info.Parallel)
	}
	for _, name := range a.sources.Names() {
		if !seen[name] {
			a.sources.SetEnabled(name, false)
		}
	}
}

// startDownloaders starts every enabled downloader in the background, so a status it reports from Init, such as
// doubledouble needing its check, shows before the first track reaches it. Startup never waits for them.
func (a *App) startDownloaders() {
	for _, e := range a.reg.Enabled(plugin.KindDownloader) {
		go func(id string) {
			h, err := a.reg.Host(id)
			if err == nil {
				err = h.Start()
			}
			if err != nil && !errors.Is(err, plugin.ErrClosed) {
				log.Printf("plugin %s: start: %v", id, err)
			}
		}(e.M.ID)
	}
}

// sourceLabel is how the UI names a source plugin's service.
func sourceLabel(e plugin.Entry) string {
	if e.M.Source == nil {
		return e.M.Name
	}
	return cmp.Or(e.M.Source.Label, e.M.Name, e.M.Source.Service)
}

// serviceLabel names service by its source plugin, on or off; the service itself when there is none.
func (a *App) serviceLabel(service string) string {
	for _, e := range a.reg.List() {
		if e.M.Kind == plugin.KindSource && e.M.Source != nil && e.M.Source.Service == service {
			return sourceLabel(e)
		}
	}
	return service
}

func (a *App) ListSources() []SourceView {
	out := []SourceView{}
	for _, e := range a.reg.Enabled(plugin.KindSource) {
		s := e.M.Source
		out = append(out, SourceView{Service: s.Service, Label: sourceLabel(e), Icon: s.Icon, Can: append([]string{}, s.Can...)})
	}
	return out
}

func (a *App) Search(service, query string, limit int) (*model.SearchResponse, error) {
	if query == "" {
		return nil, errors.New("search query is required")
	}
	if limit <= 0 {
		limit = 10
	}
	s, err := a.reg.Source(service)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	defer cancel()
	return s.Search(ctx, query, limit)
}

func (a *App) SearchByType(service, query, typ string, limit, offset int) ([]model.SearchResult, error) {
	if query == "" {
		return nil, errors.New("search query is required")
	}
	if typ == "" {
		return nil, errors.New("search type is required")
	}
	if limit <= 0 {
		limit = 50
	}
	s, err := a.reg.Source(service)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	defer cancel()
	return s.SearchByType(ctx, query, typ, limit, offset)
}

// GetPreviewURL is "" when no source of service is on or it has no previews.
func (a *App) GetPreviewURL(service, id string) (string, error) {
	s, err := a.reg.Source(service)
	if err != nil {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), sourceTimeout)
	defer cancel()
	u, err := s.Preview(ctx, id)
	if errors.Is(err, plugin.ErrCannot) {
		return "", nil
	}
	return u, err
}

// GetMetadata returns the JSON of the track, album, playlist or artist a pasted link points at.
func (a *App) GetMetadata(url string) (string, error) {
	s, ok := a.reg.SourceForLink(url)
	if !ok || !slices.Contains(s.Can, plugin.CanMetadata) {
		var labels []string
		for _, e := range a.reg.Enabled(plugin.KindSource) {
			if slices.Contains(e.M.Source.Can, plugin.CanMetadata) {
				labels = append(labels, sourceLabel(e))
			}
		}
		if len(labels) == 0 {
			return "", errors.New("Unsupported link: no source plugin is turned on.")
		}
		return "", fmt.Errorf("Unsupported link. Paste a link from %s.", orList(labels))
	}
	ctx, cancel := context.WithTimeout(context.Background(), metadataTimeout)
	defer cancel()
	raw, err := s.Metadata(ctx, url)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// needs is the service whose source a plugin depends on, "" for none.
func needs(e plugin.Entry) string {
	switch {
	case e.M.Downloader != nil && e.M.Downloader.Accepts != "any":
		return e.M.Downloader.Accepts
	case e.M.Tagger != nil:
		return e.M.Tagger.Needs
	}
	return ""
}

func (a *App) ListPlugins() []PluginView {
	list := a.reg.List()
	on := map[string]bool{} // services with an enabled source
	for _, e := range list {
		if e.Enabled && e.Err == "" && e.M.Kind == plugin.KindSource && e.M.Source != nil {
			on[e.M.Source.Service] = true
		}
	}
	out := make([]PluginView, 0, len(list))
	for _, e := range list {
		v := PluginView{ID: e.M.ID, Name: e.M.Name, Version: e.M.Version, Author: e.M.Author, Description: e.M.Description,
			Kind: string(e.M.Kind), Dir: e.Dir, Error: e.Err, Builtin: e.Builtin, Enabled: e.Enabled, Trusted: e.Trusted,
			Shadowed: append([]string{}, e.Shadowed...), Fields: append([]plugin.Field{}, e.M.Settings...), Values: e.Values,
			Dependents: []string{}}
		if v.Error == "" && e.Enabled {
			if h, err := a.reg.Host(e.M.ID); err == nil {
				v.Error = h.LastCrash()
			}
		}
		if d := e.M.Downloader; d != nil && d.Guard != nil {
			v.Fields = append(v.Fields, guardFields(*d.Guard)...)
			if _, ok := v.Values[guardLimitKey]; !ok {
				v.Values[guardLimitKey] = d.Guard.Limit
			}
			if _, ok := v.Values[guardWaitKey]; !ok {
				v.Values[guardWaitKey] = d.Guard.Wait
			}
		}
		if e.M.Kind == plugin.KindSource && e.M.Source != nil {
			for _, o := range list {
				if o.Enabled && o.Err == "" && needs(o) == e.M.Source.Service {
					v.Dependents = append(v.Dependents, o.M.ID)
				}
			}
		}
		if svc := needs(e); svc != "" && !on[svc] {
			v.Blocked = "needs source " + a.serviceLabel(svc)
		}
		out = append(out, v)
	}
	return out
}

func guardFields(g plugin.Guard) []plugin.Field {
	opts := make([]plugin.Option, len(g.Waits))
	for i, w := range g.Waits {
		opts[i] = plugin.Option{Value: w, Label: plugin.Text{"en": fmt.Sprintf("%d min", w), "ru": fmt.Sprintf("%d мин", w)}}
	}
	return []plugin.Field{
		{Key: guardLimitKey, Type: "number", Default: g.Limit,
			Label: plugin.Text{"en": "Downloads before asking", "ru": "Загрузок до вопроса"},
			Hint:  plugin.Text{"en": "0 turns the guard off", "ru": "0 — не спрашивать"}},
		{Key: guardWaitKey, Type: "select", Default: g.Wait, Options: opts,
			Label: plugin.Text{"en": "Pause after “Wait”", "ru": "Пауза после «Подождать»"}},
	}
}

func (a *App) SetPluginEnabled(id string, on, confirm bool) error {
	return a.reg.SetEnabled(id, on, confirm)
}
func (a *App) SetPluginOrder(ids []string) error { return a.reg.SetOrder(ids) }
func (a *App) RefreshPlugins()                   { a.reg.Refresh() }
func (a *App) RemovePlugin(id string) error      { return a.reg.Remove(id) }

// SetPluginSettings saves values over the plugin's saved ones; the guard fields are checked first.
func (a *App) SetPluginSettings(id string, values map[string]any) error {
	e, ok := a.reg.Get(id)
	if !ok {
		return fmt.Errorf("plugin %q: not found", id)
	}
	if d := e.M.Downloader; d != nil && d.Guard != nil {
		if v, set := values[guardLimitKey]; set {
			if n, ok := toInt(v); !ok || n < 0 {
				return errors.New("guard limit must be 0 or more")
			}
		}
		if v, set := values[guardWaitKey]; set {
			if n, ok := toInt(v); !ok || !slices.Contains(d.Guard.Waits, n) {
				return guardWaitErr(d.Guard.Waits)
			}
		}
	}
	merged := maps.Clone(a.reg.State().Settings[id])
	if merged == nil {
		merged = map[string]any{}
	}
	maps.Copy(merged, values)
	return a.reg.SetSettings(id, merged) // its OnChange applies the guard
}

// RunPluginAction runs a settings button's action, such as a browser check, under actionTimeout.
func (a *App) RunPluginAction(id, action string) error {
	h, err := a.reg.Host(id)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()
	var out struct{}
	err = h.Call(ctx, plugin.MAction, struct {
		Name string `json:"name"`
	}{action}, &out, nil)
	if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("%s: %s timed out", id, action)
	}
	return err
}

// pickZip opens a zip picker, or, on a retry, gives back the zip the last attempt left in *last.
func (a *App) pickZip(last *string, retry bool, title string) (string, error) {
	a.zipMu.Lock()
	path := *last
	a.zipMu.Unlock()
	if retry && path != "" {
		return path, nil
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: title,
		Filters: []runtime.FileFilter{{DisplayName: "Zip", Pattern: "*.zip"}}})
}

func (a *App) keepZip(last *string, path string) {
	a.zipMu.Lock()
	*last = path
	a.zipMu.Unlock()
}

// InstallPlugin installs a plugin zip the user picks; "" when cancelled. A plugin already installed answers "exists",
// and InstallPlugin(true) then replaces it from the same zip.
func (a *App) InstallPlugin(replace bool) (string, error) {
	path, err := a.pickZip(&a.installZip, replace, "Install a plugin")
	if err != nil || path == "" {
		return "", err
	}
	return a.installFrom(path, replace)
}

// installFrom installs path, remembering it when the answer is "exists" so the retry needs no second pick.
func (a *App) installFrom(path string, replace bool) (string, error) {
	id, err := a.reg.InstallZip(path, replace)
	if errors.Is(err, plugin.ErrExists) {
		a.keepZip(&a.installZip, path)
		return "", plugin.ErrExists
	}
	a.keepZip(&a.installZip, "")
	return id, err
}

// ImportPlugins imports an export the user picks and returns the ids that need the user's trust.
// Ids already installed answer "conflict:<id,id>"; ImportPlugins("replace" or "skip") then decides from the same zip.
func (a *App) ImportPlugins(choice string) ([]string, error) {
	path, err := a.pickZip(&a.importZip, choice != "", "Import plugins")
	if err != nil || path == "" {
		return []string{}, err
	}
	return a.importFrom(path, choice)
}

func (a *App) importFrom(path, choice string) ([]string, error) {
	_, need, err := a.reg.Import(path, choice)
	var c *plugin.Conflict
	if errors.As(err, &c) {
		a.keepZip(&a.importZip, path)
		return []string{}, errors.New("conflict:" + strings.Join(c.IDs, ","))
	}
	a.keepZip(&a.importZip, "")
	return append([]string{}, need...), err
}

// ExportPlugins saves the user's plugins and their settings to a zip; "" when cancelled.
func (a *App) ExportPlugins() (string, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{DefaultFilename: "sonar-plugins.zip",
		Filters: []runtime.FileFilter{{DisplayName: "Zip", Pattern: "*.zip"}}})
	if err != nil || path == "" {
		return "", err
	}
	return path, a.reg.Export(path)
}

func (a *App) OpenPluginsFolder() error {
	if err := os.MkdirAll(a.pluginsDir, 0o755); err != nil {
		return err
	}
	return a.OpenFolder(a.pluginsDir)
}
