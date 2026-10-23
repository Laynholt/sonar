package convert

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestArgs(t *testing.T) {
	joined := func(a []string) string { return strings.Join(a, " ") }

	mp3 := Args("in.flac", "out", "mp3-320", "")
	s := joined(mp3)
	for _, want := range []string{"libmp3lame", "320k", "-id3v2_version 3", "-f mp3", "-map_metadata 0", "attached_pic"} {
		if !strings.Contains(s, want) {
			t.Errorf("mp3 args %q lack %q", s, want)
		}
	}
	if mp3[len(mp3)-1] != "out" {
		t.Errorf("mp3 args must end with out: %q", s)
	}

	s = joined(Args("in.flac", "out", "m4a-aac-320", ""))
	for _, want := range []string{"aac", "-f ipod", "+faststart"} {
		if !strings.Contains(s, want) {
			t.Errorf("m4a args %q lack %q", s, want)
		}
	}
}

func TestConvertArgsGenre(t *testing.T) {
	a := Args("in.flac", "out", "mp3-320", "Rap")
	i := slices.Index(a, "-metadata")
	if i < 0 || a[i+1] != "genre=Rap" || i > len(a)-2 || a[len(a)-1] != "out" {
		t.Errorf("args %q lack -metadata genre=Rap before out", a)
	}
	if slices.Contains(Args("in.flac", "out", "m4a-aac-320", ""), "-metadata") {
		t.Error("an empty genre must not add -metadata")
	}
}

func TestExt(t *testing.T) {
	for f, want := range map[string]string{"original": ".flac", "mp3-320": ".mp3", "m4a-aac-320": ".m4a"} {
		if got := Ext(f); got != want {
			t.Errorf("Ext(%q) = %q, want %q", f, got, want)
		}
	}
}

func TestFindFFmpeg(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "myffmpeg")
	if err := os.WriteFile(custom, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindFFmpeg(custom); got != custom {
		t.Errorf("custom = %q, want %q", got, custom)
	}
	want, _ := exec.LookPath("ffmpeg")
	if got := FindFFmpeg(filepath.Join(t.TempDir(), "missing")); got != want {
		t.Errorf("missing custom = %q, want %q", got, want)
	}
}

func TestConvertOriginalIsNoop(t *testing.T) {
	in := filepath.Join(t.TempDir(), "a.flac")
	if err := os.WriteFile(in, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Convert(context.Background(), "no-such-ffmpeg", in, "original", "")
	if err != nil || got != in {
		t.Fatalf("got %q, %v", got, err)
	}
	if b, _ := os.ReadFile(in); string(b) != "data" {
		t.Error("file changed")
	}
}

func TestConvertUnknownFormat(t *testing.T) {
	in := filepath.Join(t.TempDir(), "a.flac")
	if err := os.WriteFile(in, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Convert(context.Background(), "x", in, "ogg", ""); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(in); err != nil {
		t.Errorf("input must survive: %v", err)
	}
}

func TestConvertSameExtension(t *testing.T) {
	in := filepath.Join(t.TempDir(), "a.mp3")
	if err := os.WriteFile(in, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Convert(context.Background(), "x", in, "mp3-320", "")
	if err == nil || err.Error() != "convert to mp3-320: input already has that extension" {
		t.Fatalf("err = %v", err)
	}
	if b, _ := os.ReadFile(in); string(b) != "data" {
		t.Error("input touched")
	}
}

// tools returns ffmpeg and ffprobe, skipping the test when either is missing.
func tools(t *testing.T) (ffmpeg, ffprobe string) {
	t.Helper()
	ffmpeg = FindFFmpeg("")
	if ffmpeg == "" {
		t.Skip("ffmpeg not found")
	}
	ffprobe = filepath.Join(filepath.Dir(ffmpeg), "ffprobe")
	if _, err := exec.LookPath(ffprobe); err != nil {
		if ffprobe, err = exec.LookPath("ffprobe"); err != nil {
			t.Skip("ffprobe not found")
		}
	}
	return
}

func makeFlac(t *testing.T, ffmpeg, path string, cover bool) {
	t.Helper()
	args := []string{"-y", "-loglevel", "error", "-f", "lavfi", "-i", "sine=duration=1"}
	if cover {
		args = append(args, "-f", "lavfi", "-i", "color=c=red:s=64x64:r=1:d=1", "-map", "0:a", "-map", "1:v",
			"-c:v", "mjpeg", "-disposition:v", "attached_pic")
	}
	args = append(args, "-metadata", "title=T", "-metadata", "artist=A", path)
	if out, err := exec.Command(ffmpeg, args...).CombinedOutput(); err != nil {
		t.Fatalf("make fixture: %v: %s", err, out)
	}
}

type probeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
}

type probe struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

func probeFile(t *testing.T, ffprobe, path string) probe {
	t.Helper()
	out, err := exec.Command(ffprobe, "-v", "error", "-show_entries",
		"format=duration:format_tags=title,artist:stream=codec_type,codec_name", "-of", "json", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var p probe
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConvertReal(t *testing.T) {
	ffmpeg, ffprobe := tools(t)
	for _, format := range []string{"mp3-320", "m4a-aac-320"} {
		for _, cover := range []bool{false, true} {
			name := format + "/nocover"
			if cover {
				name = format + "/cover"
			}
			t.Run(name, func(t *testing.T) {
				dir := t.TempDir()
				in := filepath.Join(dir, "x.flac")
				makeFlac(t, ffmpeg, in, cover)
				out, err := Convert(context.Background(), ffmpeg, in, format, "")
				if err != nil {
					t.Fatal(err)
				}
				if want := filepath.Join(dir, "x"+Ext(format)); out != want {
					t.Fatalf("out = %q, want %q", out, want)
				}
				if _, err := os.Stat(in); !os.IsNotExist(err) {
					t.Error("source flac not removed")
				}
				if left, _ := filepath.Glob(filepath.Join(dir, "*.part")); len(left) > 0 {
					t.Errorf("left %v", left)
				}
				p := probeFile(t, ffprobe, out)
				if p.Format.Tags["title"] != "T" || p.Format.Tags["artist"] != "A" {
					t.Errorf("tags lost: %v", p.Format.Tags)
				}
				hasStream := func(typ string) (probeStream, bool) {
					i := slices.IndexFunc(p.Streams, func(s probeStream) bool { return s.CodecType == typ })
					if i < 0 {
						return probeStream{}, false
					}
					return p.Streams[i], true
				}
				audio, ok := hasStream("audio")
				if !ok {
					t.Fatalf("no audio stream: %+v", p.Streams)
				}
				if want := map[string]string{"mp3-320": "mp3", "m4a-aac-320": "aac"}[format]; audio.CodecName != want {
					t.Errorf("audio codec = %q, want %q", audio.CodecName, want)
				}
				if d, err := strconv.ParseFloat(p.Format.Duration, 64); err != nil || d < 0.8 {
					t.Errorf("duration = %q, want >= 0.8s", p.Format.Duration)
				}
				if _, hasVideo := hasStream("video"); hasVideo != cover {
					t.Errorf("cover stream = %v, want %v", hasVideo, cover)
				}
			})
		}
	}
}

func TestConvertFailureCleansUp(t *testing.T) {
	ffmpeg, _ := tools(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "bad.flac")
	os.WriteFile(in, []byte("not audio"), 0o644)
	_, err := Convert(context.Background(), ffmpeg, in, "mp3-320", "")
	if err == nil || !strings.HasPrefix(err.Error(), "convert to mp3-320:") {
		t.Fatalf("err = %v", err)
	}
	if left, _ := os.ReadDir(dir); len(left) != 0 {
		t.Errorf("left behind: %v", left)
	}
}

func TestLastBytesKeepsRunesWhole(t *testing.T) {
	s := strings.Repeat("я", 200) // 2 bytes each; a 301-byte tail starts inside a rune
	got := lastBytes(s, 301)
	if !utf8.ValidString(got) || len(got) > 301 || got == "" {
		t.Fatalf("got %d bytes, valid=%v", len(got), utf8.ValidString(got))
	}
	if lastBytes("short", 300) != "short" {
		t.Error("short input changed")
	}
}

func TestFindFFmpegExeDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "ffmpeg"), 0o755); err != nil {
		t.Fatal(err)
	}
	inExe := filepath.Join(dir, "ffmpeg", "ffmpeg.exe")
	if err := os.WriteFile(inExe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := exeDir
	exeDir = func() string { return dir }
	defer func() { exeDir = old }()
	if got := FindFFmpeg(""); got != inExe {
		t.Errorf("exe dir = %q, want %q", got, inExe)
	}
	custom := filepath.Join(t.TempDir(), "myffmpeg")
	if err := os.WriteFile(custom, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := FindFFmpeg(custom); got != custom {
		t.Errorf("custom = %q, want %q", got, custom)
	}
}
