package amazon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"sonar/internal/httpx"
	"sonar/internal/match"
	"sonar/internal/model"
)

func httpDo(ctx context.Context, method, u string, body []byte, hdr map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", httpx.UserAgent)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := httpx.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, req.URL.Host)
	}
	return io.ReadAll(resp.Body)
}

// SearchURL is the Amazon search page for manual lookup.
func SearchURL(query string) string {
	return "https://music.amazon.com/search/" + url.PathEscape(query)
}

// Search uses the web player's guest search: no account needed.
// shortcut: unofficial API, US catalog only; breaks if Amazon changes the web player.
func Search(ctx context.Context, query string) ([]match.Item, error) {
	raw, err := showSearch(ctx, query)
	if err != nil {
		return nil, err
	}
	return parseAmazonSearch(raw)
}

func showSearch(ctx context.Context, query string) ([]byte, error) {
	cfg, err := newSession(ctx)
	if err != nil {
		return nil, err
	}

	// The API wants nested JSON strings, exactly as the web player sends them.
	js := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	headers := map[string]string{
		"x-amzn-authentication":         js(map[string]string{"interface": "ClientAuthenticationInterface.v1_0.ClientTokenElement", "accessToken": ""}),
		"x-amzn-device-model":           "WEBPLAYER",
		"x-amzn-device-width":           "1920",
		"x-amzn-device-height":          "1080",
		"x-amzn-device-family":          "WebPlayer",
		"x-amzn-device-id":              cfg.deviceID,
		"x-amzn-user-agent":             httpx.UserAgent,
		"x-amzn-session-id":             cfg.sessionID,
		"x-amzn-request-id":             strconv.FormatInt(time.Now().UnixNano(), 36),
		"x-amzn-device-language":        "en_US",
		"x-amzn-currency-of-preference": "USD",
		"x-amzn-os-version":             "1.0",
		"x-amzn-application-version":    cfg.version,
		"x-amzn-device-time-zone":       "UTC",
		"x-amzn-timestamp":              strconv.FormatInt(time.Now().UnixMilli(), 10),
		"x-amzn-csrf":                   js(map[string]string{"interface": "CSRFInterface.v1_0.CSRFHeaderElement", "token": cfg.csrfToken, "timestamp": cfg.csrfTS, "rndNonce": cfg.csrfRnd}),
		"x-amzn-music-domain":           "music.amazon.com",
		"x-amzn-referer":                "music.amazon.com",
		"x-amzn-page-url":               SearchURL(query),
	}
	body := js(map[string]string{
		"keyword":  js(map[string]string{"interface": "Web.TemplatesInterface.v1_0.Touch.SearchTemplateInterface.SearchKeywordClientInformation", "keyword": query}),
		"userHash": js(map[string]string{"level": "LIBRARY_MEMBER"}),
		"headers":  js(headers),
	})
	raw, err := httpDo(ctx, "POST", "https://na.web.skill.music.a2z.com/api/showSearch", []byte(body), map[string]string{
		"Content-Type": "text/plain;charset=UTF-8",
		"Origin":       "https://music.amazon.com",
		"Referer":      "https://music.amazon.com/",
	})
	if err != nil {
		return nil, fmt.Errorf("amazon search: %w", err)
	}
	return raw, nil
}

func parseAmazonSearch(raw []byte) ([]match.Item, error) {
	var resp struct {
		Methods []struct {
			Template struct {
				Header  string `json:"header"`
				Widgets []struct {
					Items []struct {
						PrimaryText   json.RawMessage `json:"primaryText"`
						SecondaryText json.RawMessage `json:"secondaryText"`
						Image         string          `json:"image"`
						PrimaryLink   struct {
							Deeplink string `json:"deeplink"`
						} `json:"primaryLink"`
						Tags []string `json:"tags"`
					} `json:"items"`
				} `json:"widgets"`
			} `json:"template"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("amazon search: %w", err)
	}
	if len(resp.Methods) == 0 {
		return nil, fmt.Errorf("amazon search: empty response")
	}
	t := resp.Methods[0].Template
	if t.Header == "Service error" {
		return nil, fmt.Errorf("amazon search: service returned an error")
	}

	seen := map[string]bool{}
	var out []match.Item
	for _, w := range t.Widgets {
		for _, x := range w.Items {
			link := x.PrimaryLink.Deeplink
			if !strings.HasPrefix(link, "/albums/") || seen[link] {
				continue // skips artists, playlists, podcasts
			}
			seen[link] = true
			kind := "album"
			if strings.Contains(link, "trackAsin=") {
				kind = "track"
			}
			out = append(out, match.Item{
				Kind:     kind,
				Title:    textOf(x.PrimaryText),
				Artist:   textOf(x.SecondaryText),
				Image:    x.Image,
				URL:      "https://music.amazon.com" + link,
				Explicit: slices.Contains(x.Tags, "E"),
			})
		}
	}
	return out, nil
}

// textOf accepts both "text" and {"text": "..."} forms the API mixes.
func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &o)
	return o.Text
}

// SearchCatalog maps the first page of guest search to the UI's search model.
func SearchCatalog(ctx context.Context, query string) (*model.SearchResponse, error) {
	raw, err := showSearch(ctx, query)
	if err != nil {
		return nil, err
	}
	return parseSearchResponse(raw)
}

func parseSearchResponse(raw []byte) (*model.SearchResponse, error) {
	var resp struct {
		Methods []struct {
			Template struct {
				Header  string `json:"header"`
				Widgets []struct {
					Header string `json:"header"`
					Items  []struct {
						PrimaryText struct {
							Text     string `json:"text"`
							Observer struct {
								DefaultValue struct {
									Text string `json:"text"`
								} `json:"defaultValue"`
							} `json:"observer"`
						} `json:"primaryText"`
						SecondaryText json.RawMessage `json:"secondaryText"`
						Image         string          `json:"image"`
						PrimaryLink   struct {
							Deeplink string `json:"deeplink"`
						} `json:"primaryLink"`
						Tags []string `json:"tags"`
					} `json:"items"`
				} `json:"widgets"`
			} `json:"template"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("amazon search: %w", err)
	}
	if len(resp.Methods) == 0 {
		return nil, fmt.Errorf("amazon search: empty response")
	}
	t := resp.Methods[0].Template
	if t.Header == "Service error" {
		return nil, fmt.Errorf("amazon search: service returned an error")
	}

	out := &model.SearchResponse{
		Tracks: []model.SearchResult{}, Albums: []model.SearchResult{},
		Artists: []model.SearchResult{}, Playlists: []model.SearchResult{},
	}
	for _, w := range t.Widgets {
		var kind, route string
		var dst *[]model.SearchResult
		switch w.Header {
		case "Songs":
			kind, route, dst = "track", "albums", &out.Tracks
		case "Albums":
			kind, route, dst = "album", "albums", &out.Albums
		case "Artists":
			kind, route, dst = "artist", "artists", &out.Artists
		case "Playlists":
			kind, route, dst = "playlist", "playlists", &out.Playlists
		default:
			continue // top results duplicate the lists; community playlists need an account
		}
		for _, x := range w.Items {
			path, query, _ := strings.Cut(x.PrimaryLink.Deeplink, "?")
			parts := strings.Split(strings.Trim(path, "/"), "/")
			if len(parts) < 2 || parts[0] != route {
				continue
			}
			id, u := parts[1], canonical(route, parts[1]) // artist slug dropped
			if kind == "track" {
				id = ""
				if v, err := url.ParseQuery(query); err == nil {
					id = v.Get("trackAsin")
				}
				u += "?trackAsin=" + id
			}
			if !idRe.MatchString(id) {
				continue
			}
			name := x.PrimaryText.Text
			if name == "" {
				name = x.PrimaryText.Observer.DefaultValue.Text
			}
			*dst = append(*dst, model.SearchResult{
				ID: id, Name: clean(name), Type: kind,
				Artists:     textOf(x.SecondaryText),
				Images:      x.Image,
				ExternalURL: u,
				IsExplicit:  slices.Contains(x.Tags, "E"),
			})
		}
	}
	return out, nil
}
