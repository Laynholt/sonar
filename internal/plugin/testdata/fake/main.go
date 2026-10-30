// Command fake is the plugin the host tests drive.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	s := &sdk.Server{}
	s.Init = func(p plugin.InitParams) error {
		if d, err := time.ParseDuration(os.Getenv("FAKE_INIT_SLEEP")); err == nil {
			time.Sleep(d)
		}
		mu.Lock()
		settings = p.Settings
		mu.Unlock()
		fmt.Fprintln(os.Stderr, "fake started")
		return nil
	}
	s.Notes = map[string]func(json.RawMessage){
		plugin.MSettingsChanged: func(json.RawMessage) { fmt.Fprintln(os.Stderr, "got settings") },
	}
	s.Methods = map[string]sdk.Handler{
		"echo": func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) { return p, nil },
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
		// download picks its outcome by pathNoExt: "unavailable", "needs" or "boom" fail with that code; anything else
		// reports progress and saves to pathNoExt-<title>-<amazon id>.flac.
		plugin.MDownload: func(_ context.Context, raw json.RawMessage, c *sdk.Call) (any, error) {
			var p plugin.DownloadParams
			json.Unmarshal(raw, &p)
			switch p.PathNoExt {
			case "unavailable":
				return nil, sdk.Fail(plugin.CodeUnavailable, "fake: down", nil)
			case "needs":
				return nil, sdk.Fail(plugin.CodeNeedsAction, "fake: log in", plugin.NeedsAction{Message: "Log in", Action: "login"})
			case "boom":
				return nil, sdk.Fail(plugin.CodeOther, "fake: boom", nil)
			}
			c.Progress(1, 3, "Ripping")
			c.Progress(3, 3, "")
			return plugin.DownloadResult{Path: p.PathNoExt + "-" + p.Track.Title + "-" + p.IDs["amazon"] + ".flac"}, nil
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
		plugin.MSourceSearch: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
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
			return plugin.Link{Kind: "track", ID: a.URL}, nil
		},
		plugin.MTag: func(_ context.Context, p json.RawMessage, _ *sdk.Call) (any, error) {
			var a plugin.TagParams
			json.Unmarshal(p, &a)
			return plugin.TagResult{Fields: map[string]string{"title": a.Track.Title, "isrc": a.Track.ISRC, "id": a.IDs["x"]}}, nil
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
