// Command example-go-tagger is a sonar tagger plugin template. It answers with a canned genre:
// replace the handler with your own lookup (through internal/httpx, so the user's proxy applies).
//
// It builds only inside the sonar module (sonar/internal is not importable from elsewhere):
//
//	go build -o example-go-tagger.exe ./plugins/templates/go/tagger
package main

import (
	"context"
	"fmt"
	"os"

	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

func main() {
	s := &sdk.Server{Methods: map[string]sdk.Handler{
		// tag {track, ids}: p.Track has the title, artist, album, ISRC, ...; p.IDs maps a service name to the
		// track's ID there (plugin.json tagger.needs asks sonar to find one first). Known field names: title,
		// artist, album_artist, album, track, disc, date, isrc, genre, lyrics and cover (an image URL).
		// Fill only what you have; a missing or empty value changes nothing in the file.
		plugin.MTag: sdk.Typed(func(_ context.Context, p plugin.TagParams) (any, error) {
			return plugin.TagResult{Fields: map[string]string{"genre": "Example Genre"}}, nil
		}),
	}}
	s.FollowProxy("example-go-tagger")
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "example-go-tagger:", err)
		os.Exit(1)
	}
}
