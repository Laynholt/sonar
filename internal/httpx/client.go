// Package httpx provides the app's single shared HTTP client: browser-like
// User-Agent, proxy resolution (manual > env > OS) and direct fallback.
package httpx

import (
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/145.0.0.0 Safari/537.36"

// Client returns the shared client; all outbound HTTP should go through it.
var Client = sync.OnceValue(func() *http.Client {
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: userAgentTransport{directFallbackTransport{
			primary: newProxiedHTTPTransport(),
			direct:  newDirectHTTPTransport(),
		}},
	}
})

type userAgentTransport struct{ next http.RoundTripper }

func (t userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Header.Get("User-Agent") == "" {
		req = req.Clone(req.Context()) // RoundTrippers must not mutate the caller's request
		req.Header.Set("User-Agent", UserAgent)
	}
	return t.next.RoundTrip(req)
}

func newProxiedHTTPTransport() *http.Transport {
	transport := newDirectHTTPTransport()
	transport.Proxy = proxyFor
	return transport
}

func newDirectHTTPTransport() *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// directFallbackTransport retries once without the proxy when the proxied
// attempt fails with a transient network error (dead or misconfigured proxy).
type directFallbackTransport struct {
	primary http.RoundTripper
	direct  http.RoundTripper
}

func (t directFallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.primary.RoundTrip(req)
	if err == nil || !isTransientNetworkError(err) || !canReplayRequest(req) {
		return resp, err
	}

	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}

	retryReq := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody && req.GetBody != nil {
		body, bodyErr := req.GetBody()
		if bodyErr != nil {
			return nil, err
		}
		retryReq.Body = body
	}

	return t.direct.RoundTrip(retryReq)
}

func canReplayRequest(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func isTransientNetworkError(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "proxyconnect") ||
		strings.Contains(msg, "connectex") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "timeout awaiting response headers")
}
