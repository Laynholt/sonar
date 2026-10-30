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
