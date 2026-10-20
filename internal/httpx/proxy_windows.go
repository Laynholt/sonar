//go:build windows

package httpx

import (
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

type windowsProxySettings struct {
	Enabled  bool
	Server   string
	Override string
}

func systemProxyFromOS(req *http.Request) (*url.URL, error) {
	settings, err := readWindowsProxySettings()
	if err != nil || !settings.Enabled || strings.TrimSpace(settings.Server) == "" {
		return nil, nil
	}

	host := strings.ToLower(strings.TrimSpace(req.URL.Hostname()))
	if shouldBypassWindowsProxy(host, settings.Override) {
		return nil, nil
	}

	proxySpec := pickWindowsProxyServer(settings.Server, strings.ToLower(req.URL.Scheme))
	if proxySpec == "" {
		return nil, nil
	}

	return normalizeProxyURL(proxySpec)
}

func readWindowsProxySettings() (windowsProxySettings, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return windowsProxySettings{}, err
	}
	defer key.Close()

	proxyEnable, _, _ := key.GetIntegerValue("ProxyEnable")
	proxyServer, _, _ := key.GetStringValue("ProxyServer")
	proxyOverride, _, _ := key.GetStringValue("ProxyOverride")

	return windowsProxySettings{
		Enabled:  proxyEnable != 0,
		Server:   strings.TrimSpace(proxyServer),
		Override: strings.TrimSpace(proxyOverride),
	}, nil
}
