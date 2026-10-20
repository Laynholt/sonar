package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const osProxyTTL = 30 * time.Second

var manualProxy atomic.Pointer[url.URL]

// SetManualProxy sets the user's proxy ("host:port" or a full URL); "" clears it.
func SetManualProxy(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		manualProxy.Store(nil)
		return nil
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid proxy: %w", err)
	}
	switch u.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("proxy has no host")
	}
	manualProxy.Store(u)
	return nil
}

// manualProxyString is for tests.
func manualProxyString() string {
	if u := manualProxy.Load(); u != nil {
		return u.String()
	}
	return ""
}

// isLoopback reports whether host is localhost or a loopback IP; such traffic never uses a proxy.
func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}

// proxyFor resolves the proxy for req: manual, then environment, then OS settings.
// Loopback is always direct.
func proxyFor(req *http.Request) (*url.URL, error) {
	if isLoopback(req.URL.Hostname()) {
		return nil, nil
	}
	if u := manualProxy.Load(); u != nil {
		return u, nil
	}
	if u, err := http.ProxyFromEnvironment(req); u != nil || err != nil {
		return u, err
	}
	return cachedOSProxy(req)
}

type osProxyResult struct {
	u   *url.URL
	err error
}

var osProxyCache = struct {
	sync.Mutex
	expires time.Time
	m       map[string]osProxyResult
}{}

// cachedOSProxy memoises systemProxyFromOS (which may spawn scutil/gsettings)
// per scheme+host for osProxyTTL.
func cachedOSProxy(req *http.Request) (*url.URL, error) {
	if req == nil || req.URL == nil {
		return nil, nil
	}
	key := req.URL.Scheme + "://" + req.URL.Host
	c := &osProxyCache
	c.Lock()
	defer c.Unlock()
	if now := time.Now(); now.After(c.expires) {
		c.m, c.expires = map[string]osProxyResult{}, now.Add(osProxyTTL)
	}
	if r, ok := c.m[key]; ok {
		return r.u, r.err
	}
	u, err := systemProxyFromOS(req)
	c.m[key] = osProxyResult{u, err}
	return u, err
}
