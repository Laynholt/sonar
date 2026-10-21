package download

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func gw() Track {
	return Track{ID: "t1", Title: "Global Warming", Artist: "Pitbull", Album: "Global Warming",
		AlbumArtist: "Pitbull", ReleaseDate: "2012-11-16", TrackNumber: 1, DiscNumber: 1, Position: 1}
}

func TestPathNoExtTemplates(t *testing.T) {
	noAlbum := gw()
	noAlbum.Album = ""
	noAA := gw()
	noAA.AlbumArtist = ""
	noPos := gw()
	noPos.Position = 0
	cases := []struct {
		name         string
		tr           Track
		folder, file string
		want         string
	}{
		{"full", gw(), "{artist}/[{year}] {album}", "{track}. {title}", "D/Pitbull/[2012] Global Warming/01. Global Warming"},
		{"defaults", gw(), "", "", "D/Global Warming - Pitbull"},
		{"no position", noPos, "", "{track}. {title} - {artist}", "D/Global Warming - Pitbull"},
		{"disc", gw(), "", "{disc}-{track}. {title}", "D/1-01. Global Warming"},
		{"empty album", noAlbum, "{album}", "{title}", "D/Unknown Album/Global Warming"},
		{"empty album artist", noAA, "{album_artist}", "{title}", "D/Pitbull/Global Warming"},
	}
	for _, c := range cases {
		got := PathNoExt(c.tr, Collection{Kind: "album", ID: "a", Name: "A"},
			Options{Dir: "D", FolderTemplate: c.folder, FilenameTemplate: c.file})
		if want := filepath.FromSlash(c.want); got != want {
			t.Errorf("%s: got %q, want %q", c.name, got, want)
		}
	}
}

func TestPathNoExtPlaylistFolder(t *testing.T) {
	pl := Collection{Kind: "playlist", ID: "p1", Name: "Road: Trip"}
	o := Options{Dir: "D", PlaylistFolder: true}
	if got, want := PathNoExt(gw(), pl, o), filepath.FromSlash("D/Road Trip/Global Warming - Pitbull"); got != want {
		t.Errorf("playlist: got %q, want %q", got, want)
	}
	al := Collection{Kind: "album", ID: "a", Name: "Some Album"}
	if got, want := PathNoExt(gw(), al, o), filepath.FromSlash("D/Global Warming - Pitbull"); got != want {
		t.Errorf("album: got %q, want %q", got, want)
	}
	o = Options{Dir: "D", FilenameTemplate: "{playlist} - {title}"}
	if got, want := PathNoExt(gw(), pl, o), filepath.FromSlash("D/Road Trip - Global Warming"); got != want {
		t.Errorf("placeholder: got %q, want %q", got, want)
	}
}

func TestPathNoExtSanitizes(t *testing.T) {
	c := Collection{Kind: "album", ID: "a", Name: "A"}
	cases := []struct {
		name   string
		set    func(*Track)
		folder string
		file   string
		want   string
	}{
		{"slash", func(t *Track) { t.Artist = "AC/DC" }, "{artist}", "{title}", "D/AC DC/Global Warming"},
		{"question", func(t *Track) { t.Title = "What?" }, "", "{title}", "D/What"},
		{"colon", func(t *Track) { t.Title = "Pt. 1: Intro" }, "", "{title}", "D/Pt. 1 Intro"},
		{"reserved", func(t *Track) { t.Title = "CON" }, "", "{title}", "D/_CON"},
		{"dots", func(t *Track) { t.Title = "Outro..." }, "", "{title}", "D/Outro"},
		{"blank", func(t *Track) { t.Title = "   " }, "", "{title}", "D/Unknown Title"},
	}
	for _, tc := range cases {
		tr := gw()
		tc.set(&tr)
		got := PathNoExt(tr, c, Options{Dir: "D", FolderTemplate: tc.folder, FilenameTemplate: tc.file})
		if want := filepath.FromSlash(tc.want); got != want {
			t.Errorf("%s: got %q, want %q", tc.name, got, want)
		}
	}

	long := gw()
	long.Title = strings.Repeat("é", 300)
	name := filepath.Base(PathNoExt(long, c, Options{Dir: "D", FilenameTemplate: "{title}"}))
	if n := utf8.RuneCountInString(name); n > 200 {
		t.Errorf("long title: %d runes", n)
	}

	for in, want := range map[string]string{
		"nul.txt": "_nul.txt", "COM1": "_COM1", "a\x00b": "ab", "a\xffb": "a_b", "a__b": "a_b", "": "Unknown", "console": "console",
	} {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExisting(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "song")
	if got := Existing(base, "original"); got != "" {
		t.Fatalf("empty dir: %q", got)
	}
	os.WriteFile(base+".mp3", []byte("x"), 0o644)
	if got := Existing(base, "original"); got != base+".mp3" {
		t.Errorf("mp3 only: %q", got)
	}
	os.WriteFile(base+".flac", nil, 0o644)
	if got := Existing(base, "original"); got != base+".mp3" {
		t.Errorf("0-byte flac skipped: %q", got)
	}
	os.Remove(base + ".flac")
	os.Mkdir(base+".flac", 0o755)
	if got := Existing(base, "original"); got != base+".mp3" {
		t.Errorf("dir skipped: %q", got)
	}
	os.Remove(base + ".mp3")
	if got := Existing(base, "original"); got != "" {
		t.Errorf("dir only: %q", got)
	}
	os.WriteFile(base+".m4a", []byte("x"), 0o644)
	os.WriteFile(base+".mp3", []byte("x"), 0o644)
	if got := Existing(base, "mp3-320"); got != base+".mp3" {
		t.Errorf("mp3-320 order: %q", got)
	}
	if got := Existing(base, "m4a-aac-320"); got != base+".m4a" {
		t.Errorf("m4a order: %q", got)
	}
	if got := Existing(base, "original"); got != base+".m4a" {
		t.Errorf("original order: %q", got)
	}
}
