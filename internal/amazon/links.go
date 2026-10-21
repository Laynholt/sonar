package amazon

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Link is a parsed music.amazon.* URL. Kind: "track" | "album" | "playlist" | "artist".
type Link struct{ Kind, ID string }

var ErrCommunityPlaylist = errors.New("Community playlists on Amazon Music need an account and aren't supported. Curated playlists (music.amazon.com/playlists/…) work.")

var (
	hostRe = regexp.MustCompile(`^(www\.)?music\.amazon\.[a-z.]+$`)
	idRe   = regexp.MustCompile(`^[A-Z0-9]{10}$`)
	kinds  = map[string]string{"tracks": "track", "albums": "album", "playlists": "playlist", "artists": "artist"}
)

func parseURL(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	return u, err == nil && hostRe.MatchString(strings.ToLower(u.Hostname()))
}

// IsLink reports whether raw points at any music.amazon.<tld> host, with or without a scheme.
func IsLink(raw string) bool {
	_, ok := parseURL(raw)
	return ok
}

// ParseLink is pure (no I/O) so callers can reject unsupported links offline.
func ParseLink(raw string) (Link, error) {
	u, ok := parseURL(raw)
	if !ok {
		return Link{}, fmt.Errorf("not an Amazon Music link: %q", raw)
	}
	seg := strings.Split(strings.Trim(u.Path, "/"), "/")
	if seg[0] == "my" && len(seg) > 1 {
		seg = seg[1:]
	}
	if seg[0] == "user-playlists" {
		return Link{}, ErrCommunityPlaylist
	}
	kind, known := kinds[seg[0]]
	if !known || len(seg) < 2 || !idRe.MatchString(seg[1]) {
		return Link{}, fmt.Errorf("unsupported Amazon Music link: %q", raw)
	}
	if t := u.Query().Get("trackAsin"); kind == "album" && t != "" {
		if !idRe.MatchString(t) {
			return Link{}, fmt.Errorf("unsupported Amazon Music link: %q", raw)
		}
		return Link{"track", t}, nil
	}
	return Link{kind, seg[1]}, nil
}
