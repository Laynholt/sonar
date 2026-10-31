// Command example-go-source is a sonar source plugin template. It answers with canned data:
// replace the three handlers with calls to your site (through internal/httpx, so the user's proxy applies).
//
// It builds only inside the sonar module (sonar/internal is not importable from elsewhere):
//
//	go build -o example-go-source.exe ./plugins/templates/go/source
package main

import (
	"context"
	"fmt"
	"os"

	"sonar/internal/model"
	"sonar/internal/plugin"
	"sonar/internal/plugin/sdk"
)

const service = "examplego" // must equal source.service in plugin.json

var track = model.TrackMetadata{
	Service: service, ID: "1", Name: "Example Song", Artists: "Example Artist",
	AlbumName: "Example Album", AlbumID: "10", DurationMS: 180000, ReleaseDate: "2024-01-01",
	TrackNumber: 1, ExternalURL: "https://examplego.com/track/1", // Images is a cover URL
}

func main() {
	s := &sdk.Server{Methods: map[string]sdk.Handler{
		// source.metadata {url}: the page of a pasted link. REPLACE: fetch p.URL. A track page is a
		// model.TrackResponse; albums, playlists and artists have model.AlbumResponsePayload,
		// PlaylistResponsePayload and ArtistDiscographyPayload (see docs/plugins.md).
		plugin.MSourceMetadata: sdk.Typed(func(_ context.Context, p struct{ URL string }) (any, error) {
			return model.TrackResponse{Service: service, Track: track}, nil
		}),
		// source.search {query, limit}: REPLACE with a real search of p.Query.
		plugin.MSourceSearch: sdk.Typed(func(_ context.Context, p struct {
			Query string
			Limit int
		}) (any, error) {
			hit := model.SearchResult{ID: track.ID, Name: track.Name, Type: "track", Artists: track.Artists, ExternalURL: track.ExternalURL}
			return model.SearchResponse{Tracks: []model.SearchResult{hit}, Albums: []model.SearchResult{},
				Artists: []model.SearchResult{}, Playlists: []model.SearchResult{}}, nil
		}),
		// source.parseLink {url}: kind is track, album, playlist or artist. REPLACE: parse p.URL.
		plugin.MSourceParseLink: sdk.Typed(func(_ context.Context, p struct{ URL string }) (any, error) {
			return plugin.Link{Kind: "track", ID: "1"}, nil
		}),
	}}
	s.FollowProxy("example-go-source")
	if err := sdk.Serve(s); err != nil {
		fmt.Fprintln(os.Stderr, "example-go-source:", err)
		os.Exit(1)
	}
}
