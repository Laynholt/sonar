// Command example-go-downloader is a sonar downloader plugin template: it fetches
// {baseUrl}/{artist} - {title}.flac. Replace download() with your own site or script.
//
// It builds only inside the sonar module (sonar/internal is not importable from elsewhere):
//
//	go build -o example-go-downloader.exe ./plugins/templates/go/downloader
//
// Then put the .exe and plugin.json together in a folder under %LOCALAPPDATA%\Sonar\plugins.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"sonar/internal/httpx"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

var baseURL atomic.Value // string: the baseUrl setting; $/settings can change it while a download runs

// apply re-reads what initialize and every $/settings note carry (both are a full InitParams).
func apply(p plugin.InitParams) {
	s, _ := p.Settings["baseUrl"].(string)
	baseURL.Store(s)
}

// download saves the track next to p.PathNoExt and returns the file. p.Track has the title, artist,
// album, ISRC, ...; p.IDs maps a service name to the track's ID there, for downloaders that need one.
func download(ctx context.Context, p plugin.DownloadParams, c *sdk.Call) (any, error) {
	name := p.Track.Artist + " - " + p.Track.Title + ".flac"
	base, _ := baseURL.Load().(string)
	u := strings.TrimRight(base, "/") + "/" + url.PathEscape(name)
	path := p.PathNoExt + ".flac" // the extension is yours: sonar reads the format from it

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpx.StreamClient().Do(req) // goes through the user's proxy
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err() // cancelled by $/cancel
		}
		return nil, sdk.Fail(plugin.CodeUnavailable, err.Error(), nil) // site down: sonar tries another downloader
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode >= 500:
		return nil, sdk.Fail(plugin.CodeUnavailable, fmt.Sprintf("%s: HTTP %d", u, resp.StatusCode), nil)
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: HTTP %d", u, resp.StatusCode) // an ordinary track error
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(path + ".part")
	if err != nil {
		return nil, err
	}
	var done int64
	buf := make([]byte, 256<<10) // a $/progress note per chunk; a download silent for 60 s counts as hung
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				rerr = err
			}
			done += int64(n)
			c.Progress(done, max(resp.ContentLength, 0), "") // -1: size unknown
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil { // includes ctx cancelled by $/cancel
			f.Close()
			os.Remove(path + ".part")
			return nil, rerr
		}
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(path+".part", path); err != nil {
		return nil, err
	}
	return plugin.DownloadResult{Path: path}, nil
}

func main() {
	s := &sdk.Server{}
	s.FollowProxy("example-go-downloader") // keeps httpx on the proxy sonar picked
	proxyInit, proxyNote := s.Init, s.Notes[plugin.MSettingsChanged]
	s.Init = func(p plugin.InitParams) error {
		apply(p)
		return proxyInit(p)
	}
	s.Notes[plugin.MSettingsChanged] = func(raw json.RawMessage) {
		proxyNote(raw)
		var p plugin.InitParams
		if json.Unmarshal(raw, &p) == nil {
			apply(p)
		}
	}
	s.Methods = map[string]sdk.Handler{
		plugin.MDownload: func(ctx context.Context, raw json.RawMessage, c *sdk.Call) (any, error) {
			var p plugin.DownloadParams
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("bad params: %w", err)
			}
			return download(ctx, p, c)
		},
	}
	if err := sdk.Serve(s); err != nil { // stdout is the protocol; log to stderr only
		fmt.Fprintln(os.Stderr, "example-go-downloader:", err)
		os.Exit(1)
	}
}
