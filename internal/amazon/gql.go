package amazon

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	// shortcut: public anonymous web-player key, US only; replace if Amazon rotates it (401 AUTH_*)
	gqlAPIKey = "amzn1.application.5d9d979e3a5f4e8ea83bc8536b4fde0b"
	// web player's own device type; sent with the guest session, replace if Amazon rejects it (401 AUTH_INVALID_DEVICE_*)
	gqlDeviceType = "A16ZV8BU3SN1N3"
	gqlEndpoint   = "https://gql.music.amazon.dev"
	configURL     = "https://music.amazon.com/config.json?skipToken=false&clientApplication=skyfire"
)

type session struct{ deviceID, sessionID, version, csrfToken, csrfTS, csrfRnd string }

type querier interface {
	gql(ctx context.Context, query string, vars map[string]any, out any) error
}

// newSession gets guest credentials from the web player's config endpoint.
func newSession(ctx context.Context) (*session, error) {
	raw, err := httpDo(ctx, "POST", configURL, nil, nil)
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
	return &session{cfg.DeviceID, cfg.SessionID, cfg.Version, cfg.CSRF.Token, cfg.CSRF.TS, cfg.CSRF.Rnd}, nil
}

func (s *session) gql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return err
	}
	raw, err := httpDo(ctx, "POST", gqlEndpoint, body, map[string]string{
		"Content-Type":       "application/json",
		"Origin":             "https://music.amazon.com",
		"Referer":            "https://music.amazon.com/",
		"x-api-key":          gqlAPIKey,
		"x-amzn-device-id":   s.deviceID,
		"x-amzn-device-type": gqlDeviceType,
		"music-territory":    "US",
		"x-amzn-session-id":  s.sessionID,
		"csrf-token":         s.csrfToken,
		"csrf-ts":            s.csrfTS,
		"csrf-rnd":           s.csrfRnd,
	})
	if err != nil {
		return fmt.Errorf("amazon graphql: %w", err)
	}
	return decodeGQL(raw, out)
}

// decodeGQL unmarshals "data" into out; the first GraphQL error wins.
func decodeGQL(raw []byte, out any) error {
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("amazon graphql: %w", err)
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("amazon: %s", resp.Errors[0].Message)
	}
	if len(resp.Data) == 0 {
		return fmt.Errorf("amazon: empty response")
	}
	return json.Unmarshal(resp.Data, out)
}
