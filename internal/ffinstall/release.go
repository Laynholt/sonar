// Package ffinstall downloads, verifies and installs the BtbN FFmpeg shared
// build for Windows.
package ffinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const ReleaseAPI = "https://api.github.com/repos/BtbN/FFmpeg-Builds/releases/tags/latest"

const sumsName = "checksums.sha256"

// archiveRe matches the GPL shared build only: master builds and lgpl are skipped.
var archiveRe = regexp.MustCompile(`^ffmpeg-n(\d+(?:\.\d+)*)-latest-win64-gpl-shared-\d+(?:\.\d+)*\.zip$`)

type Asset struct {
	Name, URL string
	Size      int64
}

// Latest finds the highest-versioned win64 gpl-shared archive and its checksums file.
func Latest(ctx context.Context, c *http.Client, api string) (archive, sums Asset, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err = fmt.Errorf("release lookup: HTTP %d", resp.StatusCode)
		return
	}
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		}
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rel); err != nil {
		err = fmt.Errorf("release lookup: %w", err)
		return
	}

	var best []int
	for _, a := range rel.Assets {
		if a.Name == sumsName {
			sums = Asset{a.Name, a.URL, a.Size}
		}
		m := archiveRe.FindStringSubmatch(a.Name)
		if m == nil {
			continue
		}
		var ver []int
		for _, part := range strings.Split(m[1], ".") {
			n, _ := strconv.Atoi(part)
			ver = append(ver, n)
		}
		if best == nil || slices.Compare(ver, best) > 0 {
			best, archive = ver, Asset{a.Name, a.URL, a.Size}
		}
	}
	switch {
	case best == nil:
		err = fmt.Errorf("release has no win64 gpl-shared FFmpeg archive")
	case sums.URL == "":
		err = fmt.Errorf("release has no %s", sumsName)
	}
	return
}
