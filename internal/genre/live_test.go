package genre

import (
	"net/http"

	"sonar/internal/httpx"
)

func liveClient() *http.Client { return httpx.Client() }
