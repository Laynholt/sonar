package tags

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"sonar/internal/convert"
)

func readFFmpeg(ctx context.Context, ffmpeg, path string) (Tags, bool, error) {
	cmd := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-i", path, "-f", "ffmetadata", "-")
	convert.HideWindow(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, false, ffmpegErr("read tags", err, stderr.String())
	}
	// The input listing on stderr marks a cover stream as "(attached pic)".
	return parseFFMetadata(stdout.String()), strings.Contains(stderr.String(), "(attached pic)"), nil
}

func writeFFmpeg(ctx context.Context, ffmpeg, path string, t Tags, cover []byte) error {
	ext := strings.ToLower(filepath.Ext(path))
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tags-*"+ext)
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name()) // a no-op after the rename

	args := []string{"-y", "-hide_banner", "-loglevel", "error", "-i", path}
	if cover != nil {
		ce := imageExt(cover)
		if ce == "" {
			return fmt.Errorf("cover: only JPEG and PNG are supported")
		}
		cf, err := os.CreateTemp("", "sonar-cover-*"+ce)
		if err != nil {
			return err
		}
		defer os.Remove(cf.Name())
		_, err = cf.Write(cover)
		if cerr := cf.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		args = append(args, "-i", cf.Name(), "-map", "0:a", "-map", "1:v")
	} else {
		args = append(args, "-map", "0:a", "-map", "0:v?")
	}
	// -metadata on top of -map_metadata 0 replaces just these keys; an empty value drops the tag.
	// shortcut: values travel as arguments, so a lyrics text near 30 KB hits the Windows command-line limit; use an ffmetadata file then.
	args = append(args, "-map_metadata", "0")
	for _, k := range slices.Sorted(maps.Keys(t)) {
		args = append(args, "-metadata", k+"="+t[k])
	}
	args = append(args, "-c", "copy", "-disposition:v", "attached_pic")
	if ext == ".mp3" {
		args = append(args, "-id3v2_version", "3", "-f", "mp3")
	} else {
		args = append(args, "-movflags", "+faststart", "-f", "ipod")
	}
	args = append(args, tmp.Name())

	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	convert.HideWindow(cmd)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return ffmpegErr("write tags", err, stderr.String())
	}
	return replaceFile(tmp.Name(), path)
}

func ffmpegErr(what string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	if len(msg) > 300 {
		msg = strings.ToValidUTF8(msg[len(msg)-300:], "")
	}
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("%s: %s", what, msg)
}

// parseFFMetadata reads FFmpeg's ffmetadata text (global section only) into Tags with normalised names.
func parseFFMetadata(s string) Tags {
	t, i := Tags{}, 0
	tok := func(stops string) (string, byte) { // up to an unescaped stop byte, unescaping as it goes
		var b strings.Builder
		for i < len(s) {
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(s[i+1])
				i += 2
				continue
			}
			i++
			if strings.IndexByte(stops, c) >= 0 {
				return strings.TrimSuffix(b.String(), "\r"), c
			}
			b.WriteByte(c)
		}
		return strings.TrimSuffix(b.String(), "\r"), 0
	}
	for i < len(s) && s[i] != '[' {
		if s[i] == ';' || s[i] == '#' { // header and comments
			tok("\n")
			continue
		}
		k, stop := tok("=\n")
		if stop != '=' {
			continue
		}
		v, _ := tok("\n")
		if k = normalizeKey(k); t[k] == "" {
			t[k] = v
		}
	}
	return t
}

func normalizeKey(k string) string {
	k = strings.ToLower(k)
	switch {
	case k == "tsrc":
		return "isrc"
	case strings.HasPrefix(k, "lyrics-"): // ID3 USLT carries a language
		return "lyrics"
	}
	return k
}
