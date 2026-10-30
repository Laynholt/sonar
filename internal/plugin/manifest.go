// Package plugin finds, describes and (in later files) runs sonar plugins.
package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Contract is the plugin protocol version this build speaks.
const Contract = 1

type Kind string

const (
	KindSource     Kind = "source"
	KindDownloader Kind = "downloader"
	KindTagger     Kind = "tagger"
	KindTool       Kind = "tool"
)

// Text is a string per language. In JSON it is either an object or a plain string (stored under "en").
type Text map[string]string

func (t *Text) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*t = Text{"en": s}
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("want a string or a language map: %w", err)
	}
	*t = m
	return nil
}

// In returns the text in lang, else English, else the first language by name.
func (t Text) In(lang string) string {
	if s, ok := t[lang]; ok {
		return s
	}
	if s, ok := t["en"]; ok {
		return s
	}
	keys := make([]string, 0, len(t))
	for k := range t {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return t[keys[0]]
	}
	return ""
}

type Option struct {
	Value any  `json:"value"`
	Label Text `json:"label"`
}

type Field struct {
	Key     string   `json:"key"`
	Type    string   `json:"type"` // text, number, switch, select, button, note
	Label   Text     `json:"label"`
	Hint    Text     `json:"hint"`
	Default any      `json:"default"`
	Options []Option `json:"options"`
	Action  string   `json:"action"` // button only
}

type Guard struct {
	Limit int   `json:"limit"`
	Waits []int `json:"waits"`
	Wait  int   `json:"wait"`
}

const (
	CanMetadata     = "metadata"
	CanSearch       = "search"
	CanSearchByType = "searchByType"
	CanFind         = "find"
	CanTracks       = "tracks"
	CanAlbum        = "album"
	CanISRC         = "isrc"
	CanPreview      = "preview"
	CanParseLink    = "parseLink"
)

type SourceInfo struct {
	Service string   `json:"service"`
	Label   string   `json:"label"`
	Icon    string   `json:"icon"`
	Links   []string `json:"links"` // regexps, matched against the whole pasted URL
	Can     []string `json:"can"`
}

type DownloaderInfo struct {
	Accepts  string `json:"accepts"` // a service or "any"
	Parallel int    `json:"parallel"`
	Guard    *Guard `json:"guard"`
}

type TaggerInfo struct {
	Fields []string `json:"fields"`
	Needs  string   `json:"needs"` // a service whose ID it wants, or ""
}

type Manifest struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Kind        Kind     `json:"kind"`
	Run         []string `json:"run"`
	Contract    int      `json:"contract"`

	Settings   []Field         `json:"settings"`
	Source     *SourceInfo     `json:"source"`
	Downloader *DownloaderInfo `json:"downloader"`
	Tagger     *TaggerInfo     `json:"tagger"`
}

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	fieldType = map[string]bool{"text": true, "number": true, "switch": true, "select": true, "button": true, "note": true}
)

// ParseManifest decodes and validates plugin.json. Errors name the offending field.
func ParseManifest(raw []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf")), &m); err != nil {
		return m, fmt.Errorf("manifest: %w", err)
	}
	if !idRe.MatchString(m.ID) {
		return m, fmt.Errorf("id %q: want lowercase letters, digits and dashes, 1-40 characters", m.ID)
	}
	if !versionRe.MatchString(m.Version) {
		return m, fmt.Errorf("version %q: want X.Y.Z", m.Version)
	}
	if len(m.Run) == 0 || m.Run[0] == "" {
		return m, fmt.Errorf("run: empty")
	}
	if m.Contract == 0 {
		m.Contract = 1
	}
	switch m.Kind {
	case KindSource:
		if m.Source == nil {
			return m, fmt.Errorf("source: block missing")
		}
	case KindDownloader:
		if m.Downloader == nil {
			return m, fmt.Errorf("downloader: block missing")
		}
		if m.Downloader.Accepts == "" {
			return m, fmt.Errorf("downloader.accepts: empty")
		}
		if m.Downloader.Parallel < 1 {
			m.Downloader.Parallel = 1
		}
	case KindTagger:
		if m.Tagger == nil {
			return m, fmt.Errorf("tagger: block missing")
		}
	case KindTool:
	default:
		return m, fmt.Errorf("kind %q: want source, downloader, tagger or tool", m.Kind)
	}
	for i, f := range m.Settings {
		if !fieldType[f.Type] {
			return m, fmt.Errorf("settings[%d].type %q: unknown", i, f.Type)
		}
		if f.Type == "button" && f.Action == "" {
			return m, fmt.Errorf("settings[%d].action: a button needs one", i)
		}
	}
	return m, nil
}

// CompareVersions compares X.Y.Z numerically: -1, 0 or 1. Anything unparsable counts as 0.0.0.
func CompareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := range pa {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) (p [3]int) {
	for i, s := range strings.SplitN(v, ".", 3) {
		p[i], _ = strconv.Atoi(s)
	}
	return p
}
