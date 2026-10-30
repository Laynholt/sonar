// Command lucida is sonar's built-in lucida.to downloader plugin.
package main

import (
	"fmt"
	"os"

	"sonar/internal/lucida"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

func main() {
	c := lucida.New()
	if b := os.Getenv("SONAR_LUCIDA_BASE"); b != "" {
		c.Base = b // test-only: points the plugin at a fake site
	}
	s := &sdk.Server{
		Methods: map[string]sdk.Handler{
			plugin.MDownload: sdk.DownloadHandler(func() (sdk.Downloader, error) { return c, nil }),
		},
	}
	s.FollowProxy("lucida")
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "lucida:", err)
		os.Exit(1)
	}
}
