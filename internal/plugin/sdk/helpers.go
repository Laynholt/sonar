package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"sonar/internal/httpx"
	"sonar/internal/plugin"
)

// Typed decodes a request's params into P before calling f. Params that do not decode fail
// the request with CodeOther.
func Typed[P any](f func(ctx context.Context, p P) (any, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage, _ *Call) (any, error) {
		var p P
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("bad params: %w", err)
		}
		return f(ctx, p)
	}
}

// FollowProxy makes the server apply InitParams.Proxy through httpx.SetManualProxy on initialize and
// on every $/settings note (which carries a full InitParams). It sets Init and that note; name prefixes
// the errors a note logs to stderr.
func (s *Server) FollowProxy(name string) {
	s.Init = func(p plugin.InitParams) error { return httpx.SetManualProxy(p.Proxy) }
	if s.Notes == nil {
		s.Notes = map[string]func(json.RawMessage){}
	}
	s.Notes[plugin.MSettingsChanged] = func(raw json.RawMessage) {
		var p plugin.InitParams
		if err := json.Unmarshal(raw, &p); err != nil {
			fmt.Fprintf(os.Stderr, "%s: bad $/settings: %v\n", name, err)
		} else if err := httpx.SetManualProxy(p.Proxy); err != nil {
			fmt.Fprintf(os.Stderr, "%s: proxy: %v\n", name, err)
		}
	}
}
