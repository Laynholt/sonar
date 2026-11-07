// Command lucida is sonar's built-in lucida.to downloader plugin.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync/atomic"

	"sonar/cmd/plugins/internal/dl"
	"sonar/internal/browser"
	"sonar/internal/lucida"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

// checkHints tell the user when the automatic pass failed, by language.
var checkHints = map[string]string{
	"en": "lucida is protected by Cloudflare. Press “Pass check”: it usually passes by itself in an invisible browser; if a window opens, complete the check in it. The window closes by itself.",
	"ru": "lucida закрыт проверкой Cloudflare. Нажмите «Пройти проверку»: обычно проверка проходится сама в невидимом браузере; если откроется окно, пройдите проверку в нём. Окно закроется само.",
}

func checkHint(lang string) string {
	if h, ok := checkHints[lang]; ok {
		return h
	}
	return checkHints["en"]
}

func main() {
	s := &sdk.Server{}
	// Test-only: SONAR_LUCIDA_BASE points the plugin at a fake site over plain HTTP.
	fakeBase := os.Getenv("SONAR_LUCIDA_BASE")
	var c *lucida.Client
	var bc *browser.Client

	var lang atomic.Value // string; set by Init and every $/settings
	lang.Store("en")
	var needing atomic.Bool // a check is asked for, so a new language re-sends the hint
	needs := func() plugin.NeedsAction {
		needing.Store(true)
		return plugin.NeedsAction{Message: checkHint(lang.Load().(string)), Action: "check"}
	}

	s.FollowProxy("lucida")
	proxyInit, proxyNote := s.Init, s.Notes[plugin.MSettingsChanged]
	s.Init = func(p plugin.InitParams) error {
		lang.Store(p.Lang)
		if err := proxyInit(p); err != nil {
			return err
		}
		if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
			return err
		}
		if fakeBase != "" {
			c = lucida.New()
			c.Base = fakeBase
			return nil
		}
		if bc == nil {
			bc = browser.New(p.DataDir) // lazy: no browser runs until the first download
		}
		bc.SetProxy(p.EffectiveProxy)
		if c == nil {
			if browser.FindBrowser() == "" { // without a browser the site refuses us anyway
				c = lucida.New()
			} else {
				c = lucida.NewBrowser(bc)
			}
		}
		return nil
	}
	s.Notes[plugin.MSettingsChanged] = func(raw json.RawMessage) {
		proxyNote(raw)
		var p plugin.InitParams
		if json.Unmarshal(raw, &p) == nil {
			if bc != nil {
				bc.SetProxy(p.EffectiveProxy)
			}
			if p.Lang != lang.Load().(string) {
				lang.Store(p.Lang)
				if needing.Load() {
					na := needs()
					s.Status(plugin.PluginStatus{NeedsAction: &na})
				}
			}
		}
	}
	s.Methods = map[string]sdk.Handler{
		plugin.MDownload: dl.Handler(func() (dl.Downloader, error) {
			if c == nil {
				return nil, errors.New("lucida: not initialized")
			}
			return c, nil
		}, needs),
		plugin.MAction: sdk.Typed(func(ctx context.Context, p struct{ Name string }) (any, error) {
			if p.Name != "check" {
				return nil, sdk.Fail(plugin.CodeOther, "unknown action", nil)
			}
			if c == nil {
				return nil, errors.New("lucida: not initialized")
			}
			if err := c.Check(ctx); err != nil {
				return nil, err
			}
			needing.Store(false)
			s.Status(plugin.PluginStatus{Available: true})
			return struct{}{}, nil
		}),
	}
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "lucida:", err)
		os.Exit(1)
	}
}
