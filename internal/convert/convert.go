// Package convert turns the FLAC both download sites deliver into MP3 or M4A with a local FFmpeg.
package convert

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var Formats = []string{"original", "mp3-320", "m4a-aac-320"}

// Ext is the file extension a format produces; "" for an unknown format.
func Ext(format string) string {
	switch format {
	case "original":
		return ".flac"
	case "mp3-320":
		return ".mp3"
	case "m4a-aac-320":
		return ".m4a"
	}
	return ""
}

// exeDir is a variable so tests can fake it.
var exeDir = func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Dir(exe)
}

// ExeDir is the folder of the running executable, "" if unknown.
func ExeDir() string { return exeDir() }

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// FindFFmpeg returns custom if it is an existing regular file, else <exe dir>/ffmpeg/ffmpeg.exe, else ffmpeg from PATH, else "".
func FindFFmpeg(custom string) string {
	if custom != "" && isFile(custom) {
		return custom
	}
	if d := exeDir(); d != "" {
		if p := filepath.Join(d, "ffmpeg", "ffmpeg.exe"); isFile(p) {
			return p
		}
	}
	p, _ := exec.LookPath("ffmpeg")
	return p
}

// Args are the ffmpeg arguments (without the binary) for format; the tags and the cover are kept.
func Args(in, out, format string) []string {
	a := []string{"-y", "-hide_banner", "-loglevel", "error", "-i", in,
		"-map", "0:a:0", "-map", "0:v?", "-map_metadata", "0", "-c:v", "copy", "-disposition:v", "attached_pic"}
	if format == "mp3-320" {
		return append(a, "-c:a", "libmp3lame", "-b:a", "320k", "-id3v2_version", "3", "-f", "mp3", out)
	}
	return append(a, "-c:a", "aac", "-b:a", "320k", "-movflags", "+faststart", "-f", "ipod", out)
}

// Convert converts in to format next to it and returns the new path. On success in is removed;
// on failure both in and the partial file are, so a retry downloads again.
func Convert(ctx context.Context, ffmpeg, in, format string) (string, error) {
	if format == "original" {
		return in, nil
	}
	ext := Ext(format)
	if ext == "" {
		return "", fmt.Errorf("convert to %s: unknown format", format)
	}
	out := strings.TrimSuffix(in, filepath.Ext(in)) + ext
	if out == in {
		return "", fmt.Errorf("convert to %s: input already has that extension", format)
	}
	part := out + ".part"

	cmd := exec.CommandContext(ctx, ffmpeg, Args(in, part, format)...)
	hideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		if err = os.Rename(part, out); err == nil {
			os.Remove(in)
			return out, nil
		}
	}
	os.Remove(part)
	os.Remove(in)
	msg := lastBytes(strings.TrimSpace(stderr.String()), 300)
	if msg == "" {
		msg = err.Error()
	}
	return "", fmt.Errorf("convert to %s: %s", format, msg)
}

// lastBytes keeps the end of s, dropping a rune the cut splits.
func lastBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[len(s)-n:], "")
}
