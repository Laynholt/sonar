package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"sonar/internal/httpx"
	"sonar/internal/plugin"
)

func TestTyped(t *testing.T) {
	h := Typed(func(_ context.Context, p struct{ N int }) (any, error) { return p.N + 1, nil })
	if got, err := h(context.Background(), json.RawMessage(`{"n":4}`), nil); err != nil || got != 5 {
		t.Errorf("good params: %v, %v", got, err)
	}
	for _, raw := range []string{`"nope"`, ``} {
		_, err := h(context.Background(), json.RawMessage(raw), nil)
		var pe *plugin.Error
		if err == nil || errors.As(err, &pe) || !strings.Contains(err.Error(), "bad params") {
			t.Errorf("%q: want a plain bad-params error (the server makes it CodeOther), got %v", raw, err)
		}
	}
}

func TestFollowProxy(t *testing.T) {
	t.Cleanup(func() { httpx.SetManualProxy("") })
	s := &Server{}
	s.FollowProxy("test")
	if err := s.Init(plugin.InitParams{Proxy: "127.0.0.1:8080"}); err != nil || httpx.ManualProxy() == "" {
		t.Fatalf("init: %v, proxy %q", err, httpx.ManualProxy())
	}
	s.Notes[plugin.MSettingsChanged](json.RawMessage(`{"proxy":""}`))
	if p := httpx.ManualProxy(); p != "" {
		t.Errorf("note did not clear the proxy: %q", p)
	}
	if err := s.Init(plugin.InitParams{Proxy: "ftp://x"}); err == nil {
		t.Error("a bad proxy must fail initialize")
	}
}
