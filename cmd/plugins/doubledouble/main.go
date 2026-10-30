// Command doubledouble is sonar's built-in doubledouble.top downloader plugin.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"sonar/cmd/plugins/internal/dl"
	"sonar/internal/doubledouble"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

// checkTrackURL is the page the browser check opens, the same one the in-process check used.
const checkTrackURL = "https://music.amazon.com/tracks/B00A3SGXAA"

func main() {
	var dd *doubledouble.Client // built in Init: the data dir is known only then
	var profile string
	client := func() (*doubledouble.Client, error) {
		if dd == nil {
			return nil, errors.New("doubledouble: not initialized")
		}
		return dd, nil
	}

	s := &sdk.Server{}
	s.FollowProxy("doubledouble")
	proxyInit := s.Init
	s.Init = func(p plugin.InitParams) error {
		if err := proxyInit(p); err != nil {
			return err
		}
		if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
			return err
		}
		dd = doubledouble.New(filepath.Join(p.DataDir, "token"))
		profile = filepath.Join(p.DataDir, "profile")
		if dd.Token() == "" {
			s.Status(plugin.PluginStatus{NeedsAction: &plugin.NeedsAction{Message: "Needs the browser check", Action: "check"}})
		}
		return nil
	}
	s.Methods = map[string]sdk.Handler{
		plugin.MDownload: dl.Handler(func() (dl.Downloader, error) { return client() }),
		plugin.MAction: sdk.Typed(func(ctx context.Context, p struct{ Name string }) (any, error) {
			if p.Name != "check" {
				return nil, sdk.Fail(plugin.CodeOther, "unknown action", nil)
			}
			c, err := client()
			if err != nil {
				return nil, err
			}
			browser := doubledouble.FindBrowser()
			if browser == "" {
				return nil, errors.New("no Chromium-based browser (Chrome or Edge) was found")
			}
			if err := c.Check(ctx, browser, profile, checkTrackURL); err != nil {
				return nil, err
			}
			s.Status(plugin.PluginStatus{Available: true})
			return struct{}{}, nil
		}),
	}
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "doubledouble:", err)
		os.Exit(1)
	}
}
