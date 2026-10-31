// Command fake is the plugin the host tests drive.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sonar/internal/match"
	"sonar/internal/model"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "sleep" { // the grandchild "spawn" starts
		time.Sleep(30 * time.Second)
		return
	}
	var (
		mu       sync.Mutex
		settings map[string]any
	)
	// cfg is the plugin's settings. The App tests drive the fake through its manifest's setting defaults.
	cfg := func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return settings
	}
	s := &sdk.Server{}
	s.Init = func(p plugin.InitParams) error {
		if d, err := time.ParseDuration(os.Getenv("FAKE_INIT_SLEEP")); err == nil {
			time.Sleep(d)
		}
		mu.Lock()
		settings = p.Settings
		mu.Unlock()
		if m, _ := p.Settings["initNeeds"].(string); m != "" { // like doubledouble with no token
			s.Status(plugin.PluginStatus{NeedsAction: &plugin.NeedsAction{Message: m, Action: "check"}})
		}
		fmt.Fprintln(os.Stderr, "fake started")
		return nil
	}
	s.Notes = map[string]func(json.RawMessage){
		plugin.MSettingsChanged: func(json.RawMessage) { fmt.Fprintln(os.Stderr, "got settings") },
	}
	s.Methods = map[string]sdk.Handler{
		"echo": func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) { return p, nil },
		"getenv": func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var name string
			json.Unmarshal(p, &name)
			return os.Getenv(name), nil
		},
		"progress": func(_ context.Context, _ json.RawMessage, c *sdk.Call) (any, error) {
			for i := int64(1); i <= 3; i++ {
				c.Progress(i, 3, fmt.Sprint("step ", i))
			}
			return "done", nil
		},
		"block": func(ctx context.Context, _ json.RawMessage, _ *sdk.Call) (any, error) {
			<-ctx.Done()
			fmt.Fprintln(os.Stderr, "block cancelled")
			return nil, ctx.Err()
		},
		"crash": func(context.Context, json.RawMessage, *sdk.Call) (any, error) {
			fmt.Fprintln(os.Stderr, "boom")
			os.Exit(3)
			return nil, nil
		},
		"hang": func(_ context.Context, _ json.RawMessage, c *sdk.Call) (any, error) {
			c.Progress(1, 10, "started")
			select {}
		},
		"unavailable": func(context.Context, json.RawMessage, *sdk.Call) (any, error) {
			return nil, sdk.Fail(plugin.CodeUnavailable, "down", nil)
		},
		// download picks its outcome by pathNoExt, or by the "fail" setting: "unavailable", "needs" or "boom" fail with that code,
		// "crash" exits; anything else reports progress and saves an empty pathNoExt-<title>-<amazon id>.flac, or, with the
		// "copyFrom" setting, copies that file to pathNoExt plus its extension. The "waitFor" setting holds it until that file exists, then "downloadSleep" delays it.
		plugin.MDownload: func(ctx context.Context, raw json.RawMessage, c *sdk.Call) (any, error) {
			var p plugin.DownloadParams
			json.Unmarshal(raw, &p)
			mode := p.PathNoExt
			if f, _ := cfg()["fail"].(string); f != "" {
				mode = f
			}
			switch mode {
			case "unavailable":
				return nil, sdk.Fail(plugin.CodeUnavailable, "fake: down", nil)
			case "needs":
				return nil, sdk.Fail(plugin.CodeNeedsAction, "fake: log in", plugin.NeedsAction{Message: "Log in", Action: "login"})
			case "boom":
				return nil, sdk.Fail(plugin.CodeOther, "fake: boom", nil)
			case "crash":
				os.Exit(3)
			}
			c.Progress(1, 3, "Ripping")
			if err := waitFile(ctx, cfg()["waitFor"]); err != nil {
				return nil, err
			}
			if d, err := time.ParseDuration(fmt.Sprint(cfg()["downloadSleep"])); err == nil {
				select {
				case <-time.After(d):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			c.Progress(3, 3, "")
			if from, _ := cfg()["copyFrom"].(string); from != "" {
				b, err := os.ReadFile(from)
				if err != nil {
					return nil, err
				}
				out := p.PathNoExt + filepath.Ext(from)
				if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
					return nil, err
				}
				return plugin.DownloadResult{Path: out}, os.WriteFile(out, b, 0o644)
			}
			out := p.PathNoExt + "-" + p.Track.Title + "-" + p.IDs["amazon"] + ".flac"
			if filepath.IsAbs(out) { // a relative pathNoExt gets a path and no file, which the host must refuse
				return plugin.DownloadResult{Path: out}, os.WriteFile(out, nil, 0o644)
			}
			return plugin.DownloadResult{Path: out}, nil
		},
		"junk": func(context.Context, json.RawMessage, *sdk.Call) (any, error) {
			fmt.Println("not json")
			return "ok", nil
		},
		"status": func(context.Context, json.RawMessage, *sdk.Call) (any, error) {
			s.Status(plugin.PluginStatus{NeedsAction: &plugin.NeedsAction{Message: "check", Action: "check"}})
			return "ok", nil
		},
		"spawn": func(context.Context, json.RawMessage, *sdk.Call) (any, error) {
			self, err := os.Executable()
			if err != nil {
				return nil, err
			}
			cmd := exec.Command(self, "sleep")
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr // holds the host's pipes open
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return cmd.Process.Pid, nil
		},
		// Canned answers for the per-kind clients; most echo a param, so tests see the wire names.
		plugin.MSourceMetadata: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			return p, nil
		},
		// search answers after the "searchSleep" setting's duration.
		plugin.MSourceSearch: func(ctx context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			if d, err := time.ParseDuration(fmt.Sprint(cfg()["searchSleep"])); err == nil {
				select {
				case <-time.After(d):
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			var a struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			json.Unmarshal(p, &a)
			return model.SearchResponse{Tracks: []model.SearchResult{{ID: "t1", Name: a.Query, Type: "track", Duration: a.Limit}}}, nil
		},
		plugin.MSourceSearchByType: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a struct {
				Query         string `json:"query"`
				Type          string `json:"type"`
				Limit, Offset int
			}
			json.Unmarshal(p, &a)
			return []model.SearchResult{{ID: fmt.Sprint(a.Offset), Name: a.Query, Type: a.Type, Duration: a.Limit}}, nil
		},
		plugin.MSourceFind: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			if found, ok := cfg()["find"]; ok {
				return found, nil
			}
			var a struct{ Query string }
			json.Unmarshal(p, &a)
			return []match.Item{{Kind: "track", ID: "m1", Title: a.Query, Score: 0.9}}, nil
		},
		plugin.MSourceTracks: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a struct{ IDs []string }
			json.Unmarshal(p, &a)
			var out []model.TrackInfo
			for _, id := range a.IDs {
				out = append(out, model.TrackInfo{ID: id, Title: "T " + id, ISRC: "US-" + id, DurationMS: 1000})
			}
			return out, nil
		},
		plugin.MSourceAlbum: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a struct{ ID string }
			json.Unmarshal(p, &a)
			return model.AlbumInfo{ID: a.ID, Title: "Album", Tracks: []model.TrackInfo{{ID: "t1", ISRC: "US-1"}}}, nil
		},
		plugin.MSourceISRC: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a struct{ ISRC string }
			json.Unmarshal(p, &a)
			return []string{a.ISRC + "-1", a.ISRC + "-2"}, nil
		},
		plugin.MSourcePreview: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a struct{ ID string }
			json.Unmarshal(p, &a)
			return "http://preview/" + a.ID, nil
		},
		plugin.MSourceParseLink: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a struct{ URL string }
			json.Unmarshal(p, &a)
			if strings.Contains(a.URL, "/album/") {
				return plugin.Link{Kind: "album", ID: a.URL}, nil
			}
			return plugin.Link{Kind: "track", ID: a.URL}, nil
		},
		// tag echoes its params, or, with the "tagFields" setting, answers those fields plus id_<service> for each id
		// it got, after touching the "touch" setting's file, waiting for the "waitFor" one and sleeping "tagSleep".
		plugin.MTag: func(ctx context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a plugin.TagParams
			json.Unmarshal(p, &a)
			if fields, ok := cfg()["tagFields"].(map[string]any); ok {
				if p, _ := cfg()["touch"].(string); p != "" {
					os.WriteFile(p, nil, 0o644)
				}
				if err := waitFile(ctx, cfg()["waitFor"]); err != nil {
					return nil, err
				}
				if d, err := time.ParseDuration(fmt.Sprint(cfg()["tagSleep"])); err == nil {
					select {
					case <-time.After(d):
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				out := map[string]string{}
				for k, v := range fields {
					out[k] = fmt.Sprint(v)
				}
				for svc, id := range a.IDs {
					out["id_"+svc] = id
				}
				return plugin.TagResult{Fields: out}, nil
			}
			return plugin.TagResult{Fields: map[string]string{"title": a.Track.Title, "isrc": a.Track.ISRC, "id": a.IDs["x"]}}, nil
		},
		// action "check" reports the plugin available, "lock" reports it needing the check, "wait" blocks until cancelled.
		plugin.MAction: func(ctx context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a struct{ Name string }
			json.Unmarshal(p, &a)
			switch a.Name {
			case "check":
				s.Status(plugin.PluginStatus{Available: true})
			case "lock":
				s.Status(plugin.PluginStatus{NeedsAction: &plugin.NeedsAction{Message: "Log in", Action: "check"}})
			case "wait":
				fmt.Fprintln(os.Stderr, "wait started")
				<-ctx.Done()
				return nil, ctx.Err()
			default:
				return nil, sdk.Fail(plugin.CodeOther, "unknown action", nil)
			}
			return struct{}{}, nil
		},
		"initSettings": func(context.Context, json.RawMessage, *sdk.Call) (any, error) {
			mu.Lock()
			defer mu.Unlock()
			return settings, nil
		},
	}
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// waitFile blocks until the file named by the "waitFor" setting exists: the App tests' handshake between plugins.
// A missing setting returns at once; a file that never appears fails after 20 s.
func waitFile(ctx context.Context, path any) error {
	p, _ := path.(string)
	if p == "" {
		return nil
	}
	deadline := time.After(20 * time.Second)
	for {
		if _, err := os.Stat(p); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return sdk.Fail(plugin.CodeOther, "fake: "+p+" never appeared", nil)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
