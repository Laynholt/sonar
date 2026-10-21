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
		return nil, fmt.Errorf("HTTP %d от %s", resp.StatusCode, req.URL.Host)
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
	raw, err := httpDo(ctx, "POST", "https://music.amazon.com/config.json?skipToken=false&clientApplication=skyfire", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("amazon config: %w", err)
	}
	var cfg struct {
		DeviceID  string `json:"deviceId"`
		SessionID string `json:"sessionId"`
		Version   string `json:"version"`
		CSRF      struct {
			Token string `json:"token"`
			TS    string `json:"ts"`
			Rnd   string `json:"rnd"`
		} `json:"csrf"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("amazon config: %w", err)
	}

	// The API wants nested JSON strings, exactly as the web player sends them.
	js := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	headers := map[string]string{
		"x-amzn-authentication":         js(map[string]string{"interface": "ClientAuthenticationInterface.v1_0.ClientTokenElement", "accessToken": ""}),
		"x-amzn-device-model":           "WEBPLAYER",
		"x-amzn-device-width":           "1920",
		"x-amzn-device-height":          "1080",
		"x-amzn-device-family":          "WebPlayer",
		"x-amzn-device-id":              cfg.DeviceID,
		"x-amzn-user-agent":             httpx.UserAgent,
		"x-amzn-session-id":             cfg.SessionID,
		"x-amzn-request-id":             strconv.FormatInt(time.Now().UnixNano(), 36),
		"x-amzn-device-language":        "en_US",
		"x-amzn-currency-of-preference": "USD",
		"x-amzn-os-version":             "1.0",
		"x-amzn-application-version":    cfg.Version,
		"x-amzn-device-time-zone":       "UTC",
		"x-amzn-timestamp":              strconv.FormatInt(time.Now().UnixMilli(), 10),
		"x-amzn-csrf":                   js(map[string]string{"interface": "CSRFInterface.v1_0.CSRFHeaderElement", "token": cfg.CSRF.Token, "timestamp": cfg.CSRF.TS, "rndNonce": cfg.CSRF.Rnd}),
		"x-amzn-music-domain":           "music.amazon.com",
		"x-amzn-referer":                "music.amazon.com",
		"x-amzn-page-url":               SearchURL(query),
	}
	body := js(map[string]string{
		"keyword":  js(map[string]string{"interface": "Web.TemplatesInterface.v1_0.Touch.SearchTemplateInterface.SearchKeywordClientInformation", "keyword": query}),
		"userHash": js(map[string]string{"level": "LIBRARY_MEMBER"}),
		"headers":  js(headers),
	})
	raw, err = httpDo(ctx, "POST", "https://na.web.skill.music.a2z.com/api/showSearch", []byte(body), map[string]string{
		"Content-Type": "text/plain;charset=UTF-8",
		"Origin":       "https://music.amazon.com",
		"Referer":      "https://music.amazon.com/",
	})
	if err != nil {
		return nil, fmt.Errorf("amazon search: %w", err)
	}
	return parseAmazonSearch(raw)
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
		return nil, fmt.Errorf("amazon search: пустой ответ")
	}
	t := resp.Methods[0].Template
	if t.Header == "Service error" {
		return nil, fmt.Errorf("amazon search: сервис вернул ошибку")
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
