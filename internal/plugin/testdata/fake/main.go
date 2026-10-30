// Command fake is the plugin the host tests drive.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

func main() {
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
