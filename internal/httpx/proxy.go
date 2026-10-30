package httpx

import (
	"cmp"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/http/httpproxy"
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

// ManualProxy returns the manual proxy URL, or "" when none is set.
func ManualProxy() string {
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
	if u, set, err := envProxy(req.URL); set {
		return u, err // a NO_PROXY match goes direct, not to the OS proxy
	}
	return cachedOSProxy(req)
}

// envProxy honours HTTPS_PROXY / HTTP_PROXY / ALL_PROXY (and lowercase) and
// NO_PROXY. https uses HTTPS_PROXY, else HTTP_PROXY, else ALL_PROXY; http skips
// HTTPS_PROXY. set reports whether any of those variables applies at all.
func envProxy(u *url.URL) (proxy *url.URL, set bool, err error) {
	c := httpproxy.FromEnvironment()
	all := cmp.Or(os.Getenv("ALL_PROXY"), os.Getenv("all_proxy"))
	c.HTTPProxy = cmp.Or(c.HTTPProxy, all)
	c.HTTPSProxy = cmp.Or(c.HTTPSProxy, c.HTTPProxy)
	if c.HTTPProxy == "" && c.HTTPSProxy == "" {
		return nil, false, nil
	}
	proxy, err = c.ProxyFunc()(u)
	return proxy, true, err
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

// ProxyFor returns the proxy URL the client would use for rawURL, or "" for direct.
func ProxyFor(rawURL string) string {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return ""
	}
	if u, err := proxyFor(req); err == nil && u != nil {
		return u.String()
	}
	return ""
}
