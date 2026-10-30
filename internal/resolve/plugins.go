package resolve

import (
	"context"
	"fmt"
	"slices"

	"sonar/internal/match"
	"sonar/internal/model"
	"sonar/internal/plugin"
)

// pluginFinder matches on the target source and looks ISRCs up on the origin source.
type pluginFinder struct{ origin, target *plugin.SourceClient }

func (p pluginFinder) Search(ctx context.Context, q string) ([]match.Item, error) {
	return p.target.Find(ctx, q)
}

func (p pluginFinder) LookupTracks(ctx context.Context, ids []string) ([]model.TrackInfo, error) {
	return p.target.Tracks(ctx, ids)
}

func (p pluginFinder) LookupAlbum(ctx context.Context, id string) (*model.AlbumInfo, error) {
	return p.target.Album(ctx, id)
}

func (p pluginFinder) OriginIDsByISRC(ctx context.Context, isrc string) ([]string, error) {
	return p.origin.ISRC(ctx, isrc)
}

// PluginFinders builds the Finder of a pair from the enabled source plugins of both services.
// Errors name the service that is missing or lacks the capability the match needs.
func PluginFinders(reg *plugin.Registry) Finders {
	return func(from, to string) (Finder, error) {
		origin, err := reg.Source(from)
		if err != nil {
			return nil, err
		}
		target, err := reg.Source(to)
		if err != nil {
			return nil, err
		}
		for _, need := range []struct {
			s   *plugin.SourceClient
			can string
		}{{target, "find"}, {target, "tracks"}, {target, "album"}, {origin, "isrc"}} {
			if !slices.Contains(need.s.Can, need.can) {
				return nil, fmt.Errorf("source for service %q cannot %s", need.s.Service, need.can)
			}
		}
		return pluginFinder{origin, target}, nil
	}
}
