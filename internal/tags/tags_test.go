package tags

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/go-flac/flacvorbis"
	flac "github.com/go-flac/go-flac"

	"sonar/internal/convert"
	"sonar/internal/download"
)

func TestMergeFillsOnlyEmpty(t *testing.T) {
	existing := Tags{"title": "A", "album": ""}
	base := Tags{"title": "B", "album": "X"}
	out, changed := Merge(existing, base, nil)
	if !changed || out["title"] != "A" || out["album"] != "X" {
		t.Fatalf("out %v changed %v", out, changed)
	}
	if existing["album"] != "" {
		t.Error("existing was modified")
	}

	out, _ = Merge(existing, base, []Tags{{"genre": "Pop", "title": "C"}})
	if out["title"] != "C" || out["genre"] != "Pop" || out["album"] != "X" {
		t.Errorf("tagger must win: %v", out)
	}
	out, _ = Merge(existing, base, []Tags{{"genre": "Pop"}, {"genre": "Rock", "title": ""}})
	if out["genre"] != "Rock" || out["title"] != "A" {
		t.Errorf("later tagger wins, empty never erases: %v", out)
	}

	if _, changed := Merge(Tags{"title": "A", "album": ""}, Tags{"title": "B"}, nil); changed {
		t.Error("nothing to fill must not report a change")
	}
}

func TestBaseFromTrack(t *testing.T) {
	got := Base(download.Track{Title: "T", Artist: "Ar", AlbumArtist: "AA", Album: "Al", ReleaseDate: "2020-05-01",
		TrackNumber: 3, DiscNumber: 1, ISRC: "USX1"})
	want := Tags{"title": "T", "artist": "Ar", "album_artist": "AA", "album": "Al", "date": "2020-05-01",
		"track": "3", "disc": "1", "isrc": "USX1"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v want %v", got, want)
	}
	if got := Base(download.Track{Title: "T"}); !reflect.DeepEqual(got, Tags{"title": "T"}) {
		t.Errorf("zero numbers and empty fields must be skipped: %v", got)
	}
}

// testFrames: go-flac insists the frames begin with a sync code.
var testFrames = []byte("\xff\xf8frames")

// writeTestFLAC builds the tiny FLAC the tests use (STREAMINFO, optional comments, a few frame bytes).
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
	p := filepath.Join(t.TempDir(), "tiny.flac")
	if err := f.Save(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestFLACReadWrite(t *testing.T) {
	ctx := context.Background()
	p := writeTestFLAC(t, "TITLE=old", "GENRE=Rock", "genre=Metal", "COMMENT=keep")
	if err := Write(ctx, "", p, Tags{"genre": "Pop", "title": "T", "album_artist": "AA", "track": "7"}, testPNG(t)); err != nil {
		t.Fatal(err)
	}
	got, hasCover, err := Read(ctx, "", p)
	if err != nil {
		t.Fatal(err)
	}
	want := Tags{"genre": "Pop", "title": "T", "album_artist": "AA", "track": "7", "comment": "keep"}
	if !reflect.DeepEqual(got, want) || !hasCover {
		t.Errorf("got %v cover %v, want %v with cover", got, hasCover, want)
	}

	// The old GENRE comments are replaced, not duplicated; the frames are untouched.
	f, err := flac.ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(f.Frames, testFrames) {
		t.Errorf("frames changed: %q", f.Frames)
	}
	n := 0
	for _, m := range f.Meta {
		if m.Type == flac.VorbisComment {
			vc, _ := flacvorbis.ParseFromMetaDataBlock(*m)
			for _, c := range vc.Comments {
				if strings.HasPrefix(strings.ToUpper(c), "GENRE=") {
					n++
				}
			}
		}
	}
	if n != 1 {
		t.Errorf("%d GENRE comments", n)
	}

	// Cover nil keeps the cover; an empty value removes the tag.
	if err := Write(ctx, "", p, Tags{"genre": ""}, nil); err != nil {
		t.Fatal(err)
	}
	got, hasCover, _ = Read(ctx, "", p)
	if _, ok := got["genre"]; ok || !hasCover || got["title"] != "T" {
		t.Errorf("got %v cover %v", got, hasCover)
	}

	// No comment block yet: one is created.
	q := writeTestFLAC(t)
	if err := Write(ctx, "", q, Tags{"genre": "Rap"}, nil); err != nil {
		t.Fatal(err)
	}
	if got, hasCover, _ := Read(ctx, "", q); !reflect.DeepEqual(got, Tags{"genre": "Rap"}) || hasCover {
		t.Errorf("no-block: got %v cover %v", got, hasCover)
	}
}

func TestFLACWriteKeepsOtherBlocks(t *testing.T) {
	p := writeTestFLAC(t, "TITLE=x")
	f, err := flac.ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	pad := &flac.MetaDataBlock{Type: flac.Padding, Data: make([]byte, 16)}
	f.Meta = append(f.Meta, pad)
	if err := f.Save(p); err != nil {
		t.Fatal(err)
	}
	if err := Write(context.Background(), "", p, Tags{"genre": "Rap"}, nil); err != nil {
		t.Fatal(err)
	}
	g, err := flac.ParseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range g.Meta {
		if m.Type == flac.Padding && len(m.Data) == 16 {
			return
		}
	}
	t.Fatal("padding block lost")
}

// A bad file is an error, the file stays as it was and no temp file is left behind (go-flac panics on
// metadata with no frame bytes; that must come back as an error too).
func TestFLACWriteBadFileUntouched(t *testing.T) {
	noFrames := &flac.File{Meta: []*flac.MetaDataBlock{{Type: flac.StreamInfo, Data: make([]byte, 34)}}}
	dir := t.TempDir()
	p := filepath.Join(dir, "a.flac")
	if err := noFrames.Save(p); err != nil {
		t.Fatal(err)
	}
	junk := filepath.Join(dir, "b.flac")
	if err := os.WriteFile(junk, []byte("ID3 not flac"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p, junk} {
		want, _ := os.ReadFile(f)
		if err := Write(context.Background(), "", f, Tags{"genre": "Rap"}, nil); err == nil {
			t.Errorf("%s: want error", f)
		}
		if got, _ := os.ReadFile(f); !bytes.Equal(got, want) {
			t.Errorf("%s changed", f)
		}
	}
	if es, _ := os.ReadDir(dir); len(es) != 2 {
		t.Errorf("temp file left behind: %v", es)
	}
}

func TestWriteNoFFmpeg(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.mp3")
	if err := Write(context.Background(), "", p, Tags{"genre": "Pop"}, nil); !errors.Is(err, ErrNoFFmpeg) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := Read(context.Background(), "", p); !errors.Is(err, ErrNoFFmpeg) {
		t.Fatalf("read err = %v", err)
	}
}

// makeAudio has FFmpeg synthesise a one-second file at dir/a<ext> with a COMMENT tag.
func makeAudio(t *testing.T, ffmpeg, ext string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "a"+ext)
	args := []string{"-v", "error", "-y", "-f", "lavfi", "-i", "sine=d=1", "-metadata", "comment=keep"}
	if ext == ".m4a" {
		args = append(args, "-c:a", "aac")
	}
	if out, err := exec.Command(ffmpeg, append(args, p)...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
	return p
}

func TestFFmpegReadWrite(t *testing.T) {
	ffmpeg := convert.FindFFmpeg("")
	if ffmpeg == "" {
		t.Skip("ffmpeg not found")
	}
	ctx := context.Background()
	for _, ext := range []string{".mp3", ".m4a"} {
		t.Run(ext, func(t *testing.T) {
			p := makeAudio(t, ffmpeg, ext)
			if _, hasCover, err := Read(ctx, ffmpeg, p); err != nil || hasCover {
				t.Fatalf("fresh file: cover %v, err %v", hasCover, err)
			}
			in := Tags{"title": "T", "artist": "Ar", "album": "Al", "album_artist": "AA", "genre": "Pop",
				"track": "3", "disc": "1", "date": "2020-05-01", "lyrics": "line one\nline two"}
			if ext == ".mp3" { // the M4A container has no ISRC field
				in["isrc"] = "USX123"
			}
			if err := Write(ctx, ffmpeg, p, in, testPNG(t)); err != nil {
				t.Fatal(err)
			}
			got, hasCover, err := Read(ctx, ffmpeg, p)
			if err != nil || !hasCover {
				t.Fatalf("cover %v, err %v", hasCover, err)
			}
			for k, v := range in {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
			if got["comment"] != "keep" {
				t.Errorf("other tag lost: %v", got)
			}

			// Replace one key, drop another, keep the cover.
			if err := Write(ctx, ffmpeg, p, Tags{"genre": "Rock", "lyrics": ""}, nil); err != nil {
				t.Fatal(err)
			}
			got, hasCover, err = Read(ctx, ffmpeg, p)
			if _, ok := got["lyrics"]; err != nil || got["genre"] != "Rock" || ok || got["title"] != "T" || !hasCover {
				t.Errorf("got %v cover %v err %v", got, hasCover, err)
			}
			if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
				t.Errorf("temp file left behind: %v", es)
			}
		})
	}
}

func TestFFmpegWriteFailureKeepsFile(t *testing.T) {
	ffmpeg := convert.FindFFmpeg("")
	if ffmpeg == "" {
		t.Skip("ffmpeg not found")
	}
	p := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(p, []byte("not audio"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Write(context.Background(), ffmpeg, p, Tags{"genre": "Pop"}, nil); err == nil {
		t.Fatal("want error")
	}
	if b, _ := os.ReadFile(p); string(b) != "not audio" {
		t.Error("file changed")
	}
	if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
		t.Errorf("temp file left behind: %v", es)
	}
}

func TestParseFFMetadata(t *testing.T) {
	got := parseFFMetadata(";FFMETADATA1\ntitle=a\\=b\nTSRC=US1\nlyrics-eng=x\\\ny\nbare\n[STREAM]\ntitle=no\n")
	want := Tags{"title": "a=b", "isrc": "US1", "lyrics": "x\ny"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestFetchCover(t *testing.T) {
	big := make([]byte, maxCover+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Write([]byte("img"))
		case "/big":
			w.Write(big)
		case "/exact":
			w.Write(big[:maxCover])
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if b, err := FetchCover(ctx, srv.URL+"/ok"); err != nil || string(b) != "img" {
		t.Errorf("ok: %q, %v", b, err)
	}
	if b, err := FetchCover(ctx, srv.URL+"/exact"); err != nil || len(b) != maxCover {
		t.Errorf("exactly the cap: %d bytes, %v", len(b), err)
	}
	if b, err := FetchCover(ctx, srv.URL+"/big"); err == nil || b != nil {
		t.Errorf("over the cap: %d bytes, %v", len(b), err)
	}
	if _, err := FetchCover(ctx, srv.URL+"/missing"); err == nil {
		t.Error("404 must be an error")
	}
}

// Keys Read folds together (lyrics-eng, TSRC) must be replaced by a write of the folded name, not kept beside it.
func TestFFmpegWriteReplacesFoldedKeys(t *testing.T) {
	ffmpeg := convert.FindFFmpeg("")
	if ffmpeg == "" {
		t.Skip("ffmpeg not found")
	}
	p := filepath.Join(t.TempDir(), "a.mp3")
	out, err := exec.Command(ffmpeg, "-v", "error", "-y", "-f", "lavfi", "-i", "sine=d=1", "-metadata", "lyrics-eng=old",
		"-metadata", "TSRC=US0", "-id3v2_version", "3", p).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
	ctx := context.Background()
	if got, _, _ := Read(ctx, ffmpeg, p); got["lyrics"] != "old" {
		t.Skipf("this FFmpeg did not write lyrics-eng: %v", got)
	}
	if err := Write(ctx, ffmpeg, p, Tags{"lyrics": "new", "isrc": "US1"}, nil); err != nil {
		t.Fatal(err)
	}
	got, _, err := Read(ctx, ffmpeg, p)
	if err != nil || got["lyrics"] != "new" || got["isrc"] != "US1" {
		t.Errorf("got %v, err %v", got, err)
	}
	raw, _, err := dumpFFmpeg(ctx, ffmpeg, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range raw {
		if strings.EqualFold(r.key, "lyrics-eng") || r.value == "old" || r.value == "US0" {
			t.Errorf("old frame survived: %v", raw)
		}
	}
}

func TestCancelledContextKeepsCanceled(t *testing.T) {
	ffmpeg := convert.FindFFmpeg("")
	if ffmpeg == "" {
		t.Skip("ffmpeg not found")
	}
	p := makeAudio(t, ffmpeg, ".mp3")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Write(ctx, ffmpeg, p, Tags{"genre": "Pop"}, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("write err = %v", err)
	}
	if _, _, err := Read(ctx, ffmpeg, p); !errors.Is(err, context.Canceled) {
		t.Errorf("read err = %v", err)
	}
	if es, _ := os.ReadDir(filepath.Dir(p)); len(es) != 1 {
		t.Errorf("temp file left behind: %v", es)
	}
}
