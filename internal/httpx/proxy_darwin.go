//go:build darwin

package httpx

import (
	"net/http"
	"net/url"
	"os/exec"
	"strings"
)

func systemProxyFromOS(req *http.Request) (*url.URL, error) {
	out, err := exec.Command("scutil", "--proxy").Output()
	if err != nil {
		return nil, nil
	}
	return parseScutilProxy(string(out), strings.ToLower(req.URL.Scheme)), nil
}
