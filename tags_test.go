package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// flacFile builds a minimal FLAC: STREAMINFO, a Vorbis comment and a front-cover picture.
func flacFile(t *testing.T, comments []string, cover []byte) string {
	t.Helper()
	block := func(b *bytes.Buffer, kind byte, last bool, body []byte) {
		if last {
			kind |= 0x80
		}
		n := len(body)
		b.Write([]byte{kind, byte(n >> 16), byte(n >> 8), byte(n)})
		b.Write(body)
	}
	var vc bytes.Buffer
	le := func(s string) { binary.Write(&vc, binary.LittleEndian, uint32(len(s))); vc.WriteString(s) }
	le("test vendor")
	binary.Write(&vc, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		le(c)
	}
	var pic bytes.Buffer
	be := func(v uint32) { binary.Write(&pic, binary.BigEndian, v) }
	be(3) // front cover
	be(uint32(len("image/jpeg")))
	pic.WriteString("image/jpeg")
	be(0)
	for range 4 {
		be(0)
	}
	be(uint32(len(cover)))
	pic.Write(cover)

	var f bytes.Buffer
	f.WriteString("fLaC")
	block(&f, 0, false, make([]byte, 34))
	block(&f, 4, false, vc.Bytes())
	block(&f, 6, true, pic.Bytes())
	path := filepath.Join(t.TempDir(), "t.flac")
	if err := os.WriteFile(path, f.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadTags(t *testing.T) {
	path := flacFile(t, []string{"TITLE=Song", "ARTIST=Someone", "ALBUM=Record", "DATE=2012", "TRACKNUMBER=3", "TRACKTOTAL=12", "DISCNUMBER=1/2", "ISRC=USCJY1231004", "LABEL=Big Machine"}, []byte{1, 2, 3})
	got, err := readTags(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []TagField{{"title", "Song"}, {"artist", "Someone"}, {"album", "Record"}, {"year", "2012"}, {"track", "3/12"}, {"disc", "1/2"}}
	if !slices.Equal(got.Fields, want) {
		t.Errorf("fields = %v, want %v", got.Fields, want)
	}
	if extra := []TagField{{"ISRC", "USCJY1231004"}, {"LABEL", "Big Machine"}}; !slices.Equal(got.Extra, extra) {
		t.Errorf("extra = %v, want %v", got.Extra, extra)
	}
	if got.Format != "FLAC" || got.Size == 0 || got.Path != path {
		t.Errorf("format %q size %d path %q", got.Format, got.Size, got.Path)
	}
	if got.Cover != "data:image/jpeg;base64,AQID" {
		t.Errorf("cover = %q", got.Cover)
	}
}

func TestReadTagsNotAudio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.flac")
	os.WriteFile(path, []byte("not a flac at all"), 0o644)
	if _, err := readTags(path); err == nil {
		t.Error("want an error for a file without tags")
	}
}
