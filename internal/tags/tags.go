// Package tags reads and writes audio tags: base tags from the page data and fields from tagger plugins,
// into FLAC (Vorbis comments, in-process) and MP3 or M4A (through FFmpeg).
package tags

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sonar/internal/download"
	"sonar/internal/httpx"
)

// Tags maps a tag name to its value. The known names are title, artist, album_artist, album, track, disc,
// date, isrc, genre and lyrics; others pass through.
type Tags map[string]string

// ErrNoFFmpeg is returned when an MP3 or M4A has to be read or written and FFmpeg is not available.
var ErrNoFFmpeg = errors.New("FFmpeg not found: tags not written")

const maxCover = 10 << 20

// Base is what the page data says about t; empty values and zero numbers are left out.
func Base(t download.Track) Tags {
	b := Tags{}
	set := func(k, v string) {
		if v != "" {
			b[k] = v
		}
	}
	set("title", t.Title)
	set("artist", t.Artist)
	set("album_artist", t.AlbumArtist)
	set("album", t.Album)
	if t.TrackNumber > 0 {
		set("track", strconv.Itoa(t.TrackNumber))
	}
	if t.DiscNumber > 0 {
		set("disc", strconv.Itoa(t.DiscNumber))
	}
	set("date", t.ReleaseDate)
	set("isrc", t.ISRC)
	return b
}

// Merge fills the empty values of existing from base, then lets each tagger overwrite, in order
// (a tagger's empty value never erases). changed reports whether out differs from existing.
func Merge(existing, base Tags, taggers []Tags) (out Tags, changed bool) {
	out = maps.Clone(existing)
	if out == nil {
		out = Tags{}
	}
	for k, v := range base {
		if out[k] == "" && v != "" {
			out[k] = v
		}
	}
	for _, tg := range taggers {
		for k, v := range tg {
			if v != "" {
				out[k] = v
			}
		}
	}
	return out, !maps.Equal(out, existing)
}

// Read returns the tags of the file at path and whether it has a cover. ffmpeg is needed for MP3 and M4A only.
func Read(ctx context.Context, ffmpeg, path string) (t Tags, hasCover bool, err error) {
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".flac":
		return readFLAC(path)
	case ".mp3", ".m4a":
		if ffmpeg == "" {
			return nil, false, ErrNoFFmpeg
		}
		return readFFmpeg(ctx, ffmpeg, path)
	default:
		return nil, false, fmt.Errorf("tags: unsupported file type %q", ext)
	}
}

// Write sets the given tags of the file at path (an empty value removes the tag, other tags stay) and, if cover
// is not nil, replaces the front cover. It writes a temp file next to path and renames it over, so a failure
// leaves path untouched.
func Write(ctx context.Context, ffmpeg, path string, t Tags, cover []byte) error {
	switch ext := strings.ToLower(filepath.Ext(path)); ext {
	case ".flac":
		return writeFLAC(path, t, cover)
	case ".mp3", ".m4a":
		if ffmpeg == "" {
			return ErrNoFFmpeg
		}
		return writeFFmpeg(ctx, ffmpeg, path, t, cover)
	default:
		return fmt.Errorf("tags: unsupported file type %q", ext)
	}
}

// FetchCover downloads a cover image; one over 10 MB is an error, not a truncated picture.
func FetchCover(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpx.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxCover+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxCover {
		return nil, errors.New("cover: image is over 10 MB")
	}
	return b, nil
}

// imageExt is the file extension of a cover FLAC and FFmpeg both take; "" for anything else.
func imageExt(cover []byte) string {
	switch http.DetectContentType(cover) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	}
	return ""
}

// replaceFile gives tmp the mode of path and renames it over path; on failure tmp is removed.
func replaceFile(tmp, path string) error {
	st, err := os.Stat(path)
	if err == nil {
		err = os.Chmod(tmp, st.Mode().Perm())
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
