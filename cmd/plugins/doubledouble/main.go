// Command doubledouble is sonar's built-in doubledouble.top downloader plugin.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"sonar/cmd/plugins/internal/dl"
	"sonar/internal/doubledouble"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

// checkTrackURL is the page the browser check opens, the same one the in-process check used.
const checkTrackURL = "https://music.amazon.com/tracks/B00A3SGXAA"

// checkHints tell the user what to do in the check's browser window, by language.
var checkHints = map[string]string{
	"en": "In that window: untick “Upload to”, tick “Hide my download”, press Download and pass the Cloudflare check. The window closes by itself. Sonar's own downloads always skip both.",
	"ru": "В этом окне снимите галку «Upload to», поставьте «Hide my download», нажмите Download и пройдите проверку Cloudflare. Окно закроется само. Загрузки самого Sonar всегда идут без заливки и без показа в недавних.",
}

// checkHint is the hint in lang, English for any language it has none in.
func checkHint(lang string) string {
	if h, ok := checkHints[lang]; ok {
		return h
	}
	return checkHints["en"]
}

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
	var lang atomic.Value // string; set by Init and every $/settings
	lang.Store("en")
	var needing atomic.Bool // a check is asked for, so a new language re-sends the hint
	needs := func() plugin.NeedsAction {
		needing.Store(true)
		return plugin.NeedsAction{Message: checkHint(lang.Load().(string)), Action: "check"}
	}
	s.FollowProxy("doubledouble")
	proxyInit, proxyNote := s.Init, s.Notes[plugin.MSettingsChanged]
	s.Init = func(p plugin.InitParams) error {
		lang.Store(p.Lang)
		if err := proxyInit(p); err != nil {
			return err
		}
		if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
			return err
		}
		dd = doubledouble.New(filepath.Join(p.DataDir, "token"))
		profile = filepath.Join(p.DataDir, "profile")
		if dd.Token() == "" {
			na := needs()
			s.Status(plugin.PluginStatus{NeedsAction: &na})
		}
		return nil
	}
	s.Notes[plugin.MSettingsChanged] = func(raw json.RawMessage) {
		proxyNote(raw)
		var p plugin.InitParams
		if json.Unmarshal(raw, &p) == nil && p.Lang != lang.Load().(string) {
			lang.Store(p.Lang)
			if needing.Load() {
				na := needs()
				s.Status(plugin.PluginStatus{NeedsAction: &na})
			}
		}
	}
	s.Methods = map[string]sdk.Handler{
		plugin.MDownload: dl.Handler(func() (dl.Downloader, error) { return client() }, needs),
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
			needing.Store(false)
			s.Status(plugin.PluginStatus{Available: true})
			return struct{}{}, nil
		}),
	}
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "doubledouble:", err)
		os.Exit(1)
	}
}
