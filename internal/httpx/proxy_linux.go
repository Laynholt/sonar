//go:build linux

package httpx

import (
	"net/http"
	"net/url"
	"os/exec"
	"strings"
)

func systemProxyFromOS(req *http.Request) (*url.URL, error) {
	get := func(schema, key string) string {
		out, err := exec.Command("gsettings", "get", schema, key).Output()
		if err != nil {
			return ""
		}
		return string(out)
	}
	return parseGSettingsProxy(get, strings.ToLower(req.URL.Scheme)), nil
}
