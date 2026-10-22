package main

import (
	"encoding/base64"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

type TagField struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TrackTags is what a downloaded file carries. Fields use fixed keys the UI translates;
// Extra holds the file's other tags under their own names.
type TrackTags struct {
	Path   string     `json:"path"`
	Format string     `json:"format"`
	Size   int64      `json:"size"`
	Fields []TagField `json:"fields"`
	Extra  []TagField `json:"extra"`
	Cover  string     `json:"cover"` // data URL, "" when there is none
}

// GetDownloadTags reads the tags of a finished download by its queue id.
func (a *App) GetDownloadTags(id string) (TrackTags, error) {
	for _, it := range a.dl.Snapshot() {
		if it.ID == id && it.Path != "" {
			return readTags(it.Path)
		}
	}
	return TrackTags{}, errors.New("this download has no file yet")
}

// Vorbis comments the standard fields already show.
var vorbisShown = []string{"title", "artist", "album", "albumartist", "composer", "genre", "year",
	"tracknumber", "tracktotal", "totaltracks", "discnumber", "disctotal", "totaldiscs", "comment", "metadata_block_picture", "vendor"}

func readTags(path string) (TrackTags, error) {
	f, err := os.Open(path)
	if err != nil {
		return TrackTags{}, err
	}
	defer f.Close()
	out := TrackTags{Path: path}
	if fi, err := f.Stat(); err == nil {
		out.Size = fi.Size()
	}
	m, err := tag.ReadFrom(f)
	if err != nil {
		return TrackTags{}, err
	}
	out.Format = string(m.FileType())
	pos := func(n, total int) string {
		switch {
		case n == 0:
			return ""
		case total == 0:
			return strconv.Itoa(n)
		}
		return strconv.Itoa(n) + "/" + strconv.Itoa(total)
	}
	year := ""
	if m.Year() != 0 {
		year = strconv.Itoa(m.Year())
	}
	track, tracks := m.Track()
	disc, discs := m.Disc()
	composer, trackText, discText := m.Composer(), pos(track, tracks), pos(disc, discs)
	if m.Format() == tag.VORBIS {
		raw := func(k string) string { s, _ := m.Raw()[k].(string); return s }
		// The library falls back to the artist, and reads "1/12" as no number.
		composer = raw("composer")
		vorbisPos := func(num string, totals ...string) string {
			n := raw(num)
			for _, k := range totals {
				if t := raw(k); n != "" && t != "" && !strings.Contains(n, "/") {
					return n + "/" + t
				}
			}
			return n
		}
		trackText, discText = vorbisPos("tracknumber", "tracktotal", "totaltracks"), vorbisPos("discnumber", "disctotal", "totaldiscs")
	}
	for _, f := range []TagField{
		{"title", m.Title()}, {"artist", m.Artist()}, {"album", m.Album()}, {"album_artist", m.AlbumArtist()},
		{"composer", composer}, {"genre", m.Genre()}, {"year", year},
		{"track", trackText}, {"disc", discText}, {"comment", m.Comment()},
	} {
		if f.Value != "" {
			out.Fields = append(out.Fields, f)
		}
	}
	// shortcut: extra tags only for FLAC/OGG, whose keys are readable names; MP3/M4A show the standard
	// fields only. Map ID3 frames and MP4 atoms if those formats need more.
	if m.Format() == tag.VORBIS {
		for k, v := range m.Raw() {
			s, ok := v.(string)
			if !ok || s == "" || slices.Contains(vorbisShown, k) || (k == "date" && s == year) {
				continue
			}
			out.Extra = append(out.Extra, TagField{strings.ToUpper(k), s})
		}
		slices.SortFunc(out.Extra, func(a, b TagField) int { return strings.Compare(a.Key, b.Key) })
	}
	if p := m.Picture(); p != nil && len(p.Data) > 0 {
		out.Cover = "data:" + p.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(p.Data)
	}
	return out, nil
}
