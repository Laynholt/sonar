//go:build !windows && !darwin && !linux

package httpx

import (
	"net/http"
	"net/url"
)

func systemProxyFromOS(_ *http.Request) (*url.URL, error) { return nil, nil }
