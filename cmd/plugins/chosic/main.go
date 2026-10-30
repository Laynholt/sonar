// Command chosic is sonar's built-in genre tagger: the artist's genres from chosic.com.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"sonar/internal/genre"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

func main() {
	var cache *genre.Cache // opened in Init: the data dir is known only then
	var single atomic.Bool // the singleGenre switch; $/settings changes it while tags run
	c := &genre.Client{}
	if b := os.Getenv("SONAR_CHOSIC_BASE"); b != "" {
		c.Base = b // test-only: points the plugin at a fake site
	}
	apply := func(p plugin.InitParams) { single.Store(p.Settings["singleGenre"] == true) }

	s := &sdk.Server{}
	s.FollowProxy("chosic")
	proxyInit, proxyNote := s.Init, s.Notes[plugin.MSettingsChanged]
	s.Init = func(p plugin.InitParams) error {
		if err := proxyInit(p); err != nil {
			return err
		}
		if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
			return err
		}
		cache = genre.OpenCache(filepath.Join(p.DataDir, "genres.json"))
		apply(p)
		return nil
	}
	s.Notes[plugin.MSettingsChanged] = func(raw json.RawMessage) {
		proxyNote(raw)
		var p plugin.InitParams
		if json.Unmarshal(raw, &p) == nil {
			apply(p)
		}
	}
	s.Methods = map[string]sdk.Handler{
		plugin.MTag: sdk.Typed(func(ctx context.Context, p plugin.TagParams) (any, error) {
			res := plugin.TagResult{Fields: map[string]string{}}
			id := p.IDs["spotify"]
			if id == "" {
				return res, nil
			}
			g, ok := cache.Get(id)
			if !ok {
				var err error
				if g, err = c.Genres(ctx, id); err != nil {
					return nil, err
				}
				cache.Put(id, g)
			}
			if f := genre.Format(g, single.Load()); f != "" {
				res.Fields["genre"] = f
			}
			return res, nil
		}),
	}
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "chosic:", err)
		os.Exit(1)
	}
}
