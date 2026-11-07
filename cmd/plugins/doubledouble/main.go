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
	"sonar/internal/browser"
	"sonar/internal/doubledouble"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

// checkHints tell the user what to do when the automatic pass failed, by language.
var checkHints = map[string]string{
	"en": "doubledouble needs a CAPTCHA token. Press “Pass check”: it usually passes by itself in an invisible browser; if a window opens, pass the Cloudflare check in it. The window closes by itself. Sonar's own downloads always skip uploading and stay out of “recently downloaded”.",
	"ru": "doubledouble нужен CAPTCHA-токен. Нажмите «Пройти проверку»: обычно проверка проходится сама в невидимом браузере; если откроется окно, пройдите проверку Cloudflare в нём. Окно закроется само. Загрузки Sonar всегда идут без заливки и без показа в недавних.",
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
	var bc *browser.Client
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
		if dd == nil {
			dd = doubledouble.New(filepath.Join(p.DataDir, "token"))
			// Test-only: SONAR_DD_BASE points the plugin at a fake site over plain
			// HTTP, with no browser and no auto-solving.
			if fakeBase := os.Getenv("SONAR_DD_BASE"); fakeBase != "" {
				dd.Base = fakeBase
			} else {
				if bc == nil {
					bc = browser.New(p.DataDir) // lazy: no browser runs until a download or a check
				}
				bc.SetProxy(p.EffectiveProxy)
				dd.Solver = doubledouble.BrowserSolver{C: dd, BC: bc}
			}
		} else if bc != nil {
			bc.SetProxy(p.EffectiveProxy)
		}
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
			if bc == nil { // the fake-site test hook runs without a browser
				return nil, sdk.Fail(plugin.CodeUnavailable, "no browser for the check", nil)
			}
			if err := c.CheckVisibleToken(ctx, bc); err != nil {
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
