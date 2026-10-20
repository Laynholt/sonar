package httpx

// Untagged so the parsers are testable on every OS.

import (
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// parseScutilProxy reads `scutil --proxy` output: the HTTPS (or HTTP) proxy for
// scheme, falling back to SOCKS.
func parseScutilProxy(out, scheme string) *url.URL {
	kv := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok {
			kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	pick := func(prefix, urlScheme string) *url.URL {
		if kv[prefix+"Enable"] != "1" {
			return nil
		}
		return hostPortURL(urlScheme, kv[prefix+"Proxy"], kv[prefix+"Port"])
	}
	main := "HTTP"
	if scheme == "https" {
		main = "HTTPS"
	}
	if u := pick(main, "http"); u != nil {
		return u
	}
	return pick("SOCKS", "socks5")
}

// parseGSettingsProxy reads GNOME proxy settings through get(schema, key), which
// returns gsettings' raw output ("" on error).
func parseGSettingsProxy(get func(schema, key string) string, scheme string) *url.URL {
	const base = "org.gnome.system.proxy"
	if strings.Trim(strings.TrimSpace(get(base, "mode")), "'") != "manual" {
		return nil
	}
	pick := func(name, urlScheme string) *url.URL {
		schema := base + "." + name
		host := strings.Trim(strings.TrimSpace(get(schema, "host")), "'")
		return hostPortURL(urlScheme, host, strings.TrimSpace(get(schema, "port")))
	}
	main := "http"
	if scheme == "https" {
		main = "https"
	}
	if u := pick(main, "http"); u != nil {
		return u
	}
	return pick("socks", "socks5")
}

// hostPortURL returns nil for an empty host or a missing/zero port.
func hostPortURL(scheme, host, port string) *url.URL {
	if p, err := strconv.Atoi(port); host == "" || err != nil || p <= 0 {
		return nil
	}
	return &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, port)}
}

func shouldBypassWindowsProxy(host string, overrideList string) bool {
	if host == "" || strings.TrimSpace(overrideList) == "" {
		return false
	}

	rules := strings.FieldsFunc(overrideList, func(r rune) bool {
		return r == ';' || r == ','
	})

	for _, rule := range rules {
		rule = strings.ToLower(strings.TrimSpace(rule))
		if rule == "" {
			continue
		}

		if rule == "<local>" && !strings.Contains(host, ".") {
			return true
		}

		if strings.HasPrefix(rule, ".") && strings.HasSuffix(host, rule) {
			return true
		}

		if rule == host {
			return true
		}

		if matched, err := path.Match(rule, host); err == nil && matched {
			return true
		}
	}

	return false
}

func pickWindowsProxyServer(proxyServer string, requestScheme string) string {
	trimmed := strings.TrimSpace(proxyServer)
	if trimmed == "" {
		return ""
	}

	if !strings.Contains(trimmed, "=") {
		return trimmed
	}

	proxies := make(map[string]string)
	firstValue := ""

	for _, part := range strings.Split(trimmed, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(kv[0]))
		value := strings.TrimSpace(kv[1])
		if key == "" || value == "" {
			continue
		}

		if firstValue == "" {
			firstValue = value
		}
		proxies[key] = value
	}

	if direct := proxies[requestScheme]; direct != "" {
		return direct
	}

	if requestScheme == "https" {
		if fallback := proxies["http"]; fallback != "" {
			return fallback
		}
	}

	if socks := proxies["socks"]; socks != "" {
		return ensureSocksScheme(socks)
	}
	if socks5 := proxies["socks5"]; socks5 != "" {
		return ensureSocksScheme(socks5)
	}

	return firstValue
}

func ensureSocksScheme(proxyValue string) string {
	if strings.Contains(proxyValue, "://") {
		return proxyValue
	}
	return "socks5://" + proxyValue
}

func normalizeProxyURL(proxySpec string) (*url.URL, error) {
	proxySpec = strings.TrimSpace(proxySpec)
	if proxySpec == "" {
		return nil, nil
	}

	if !strings.Contains(proxySpec, "://") {
		proxySpec = "http://" + proxySpec
	}

	proxyURL, err := url.Parse(proxySpec)
	if err != nil {
		return nil, nil
	}

	if proxyURL.Host == "" && proxyURL.Path != "" {
		proxyURL.Host = proxyURL.Path
		proxyURL.Path = ""
	}

	if proxyURL.Host == "" {
		return nil, nil
	}

	return proxyURL, nil
}
