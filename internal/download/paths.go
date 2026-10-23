// Package download turns tracks into files on disk.
package download

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Track struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Album       string `json:"album"`
	AlbumArtist string `json:"album_artist"`
	ReleaseDate string `json:"release_date"`
	TrackNumber int    `json:"track_number"`
	DiscNumber  int    `json:"disc_number"`
	Position    int    `json:"position"`
	Service     string `json:"service"` // "spotify" or "amazon"; "" means amazon
	AlbumID     string `json:"album_id"`
	DurationMS  int    `json:"duration_ms"`
	Explicit    bool   `json:"explicit"`
	Image       string `json:"image"`
	Group       string `json:"group"` // same non-empty Group = matched together as one album
}

// Collection is what the user asked to download; Kind is track, album, playlist or artist.
type Collection struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Options struct {
	Dir              string `json:"dir"`
	Format           string `json:"format"`
	Source           string `json:"source"`
	FolderTemplate   string `json:"folder_template"`
	FilenameTemplate string `json:"filename_template"`
	FFmpegPath       string `json:"ffmpeg_path"`
	PlaylistFolder   bool   `json:"playlist_folder"`
	GuardLimit       int    `json:"guard_limit"`
	GuardWait        int    `json:"guard_wait"` // minutes a site waits after "Wait": 15, 30, 60 or 120
	EmbedGenre       bool   `json:"embed_genre"`
	SingleGenre      bool   `json:"single_genre"`
}

// PathNoExt is the target path of a track without its extension.
func PathNoExt(t Track, c Collection, o Options) string {
	title, artist, album := orElse(t.Title, "Unknown Title"), orElse(t.Artist, "Unknown Artist"), orElse(t.Album, "Unknown Album")
	year, date, disc := "0000", orElse(t.ReleaseDate, "0000-00-00"), 1
	if len(t.ReleaseDate) >= 4 {
		year = t.ReleaseDate[:4]
	}
	if t.DiscNumber > 0 {
		disc = t.DiscNumber
	}
	playlist := ""
	if c.Kind == "playlist" {
		playlist = c.Name
	}
	// One replacer pass keeps a value that itself contains "{artist}" literal; longer removals come first.
	pairs := []string{
		"{title}", Sanitize(title), "{artist}", Sanitize(artist), "{album}", Sanitize(album),
		"{album_artist}", Sanitize(orElse(t.AlbumArtist, artist)), "{year}", Sanitize(year),
		"{date}", Sanitize(date), "{disc}", fmt.Sprint(disc),
	}
	if playlist == "" {
		pairs = append(pairs, "{playlist} - ", "", "{playlist}. ", "", "{playlist}", "")
	} else {
		pairs = append(pairs, "{playlist}", Sanitize(playlist))
	}
	if t.Position <= 0 {
		pairs = append(pairs, "{track}. ", "", "{track} - ", "", "{track}", "")
	} else {
		pairs = append(pairs, "{track}", fmt.Sprintf("%02d", t.Position))
	}
	fill := strings.NewReplacer(pairs...).Replace

	base := o.Dir
	if o.PlaylistFolder && c.Kind == "playlist" && c.Name != "" {
		base = filepath.Join(base, Sanitize(c.Name))
	}
	for _, seg := range strings.FieldsFunc(fill(o.FolderTemplate), func(r rune) bool { return r == '/' || r == '\\' }) {
		base = filepath.Join(base, Sanitize(seg))
	}
	return filepath.Join(base, Sanitize(fill(orElse(o.FilenameTemplate, "{title} - {artist}"))))
}

func orElse(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// Sanitize makes s safe as one path segment on Windows and elsewhere.
func Sanitize(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch {
		case r == utf8.RuneError && n == 1:
			b.WriteByte('_')
		case strings.ContainsRune(`/\:*?"<>|`, r) || unicode.IsSpace(r):
			b.WriteByte(' ')
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	for strings.Contains(out, "__") {
		out = strings.ReplaceAll(out, "__", "_")
	}
	trim := func(s string) string { return strings.Trim(s, " .") }
	out = trim(out)
	if r := []rune(out); len(r) > 200 {
		out = trim(string(r[:200]))
	}
	if out == "" {
		return "Unknown"
	}
	stem, _, _ := strings.Cut(out, ".")
	switch s := strings.ToLower(strings.TrimRight(stem, " ")); s {
	case "con", "prn", "aux", "nul",
		"com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9",
		"lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9":
		return "_" + out
	}
	return out
}

// Existing returns the already downloaded file for pathNoExt, or "".
func Existing(pathNoExt, format string) string {
	exts := []string{".flac", ".m4a", ".mp3"}
	switch format {
	case "mp3-320":
		exts = []string{".mp3", ".m4a", ".flac"}
	case "m4a-aac-320":
		exts = []string{".m4a", ".mp3", ".flac"}
	}
	for _, e := range exts {
		if st, err := os.Stat(pathNoExt + e); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
			return pathNoExt + e
		}
	}
	return ""
}
