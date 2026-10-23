package genre

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"
)

func writeTestFLAC(t *testing.T, comments ...string) string {
	t.Helper()
	f := &flac.File{
		Meta:   []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}},
		Frames: testFrames,
	}
	if len(comments) > 0 {
		vc := flacvorbis.New()
		vc.Comments = comments
		b := vc.Marshal()
		f.Meta = append(f.Meta, &b)
	}
	p := filepath.Join(t.TempDir(), "a.flac")
	if err := f.Save(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func readComments(t *testing.T, p string) []string {
	t.Helper()
	f, err := flac.ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.Frames, testFrames) {
		t.Fatalf("frames changed: %q", f.Frames)
	}
	for _, m := range f.Meta {
		if m.Type == flac.VorbisComment {
			vc, err := flacvorbis.ParseFromMetaDataBlock(*m)
			if err != nil {
				t.Fatal(err)
			}
			return vc.Comments
		}
	}
	return nil
}

func TestWriteFLAC(t *testing.T) {
	p := writeTestFLAC(t, "TITLE=x", "GENRE=old", "genre=older")
	if err := WriteFLAC(p, "Rap"); err != nil {
		t.Fatal(err)
	}
	if got, want := readComments(t, p), []string{"TITLE=x", "GENRE=Rap"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}

	p = writeTestFLAC(t)
	if err := WriteFLAC(p, "Rap"); err != nil {
		t.Fatal(err)
	}
	if got, want := readComments(t, p), []string{"GENRE=Rap"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no-block: got %q want %q", got, want)
	}
}

func TestWriteFLACNotFLAC(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(p, []byte("ID3 not flac"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFLAC(p, "Rap"); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(p); string(b) != "ID3 not flac" {
		t.Fatalf("file changed: %q", b)
	}
	if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
		t.Fatalf("temp file left behind: %v", es)
	}
}

// go-flac insists the frames begin with a sync code.
var testFrames = []byte("\xff\xf8frames")
