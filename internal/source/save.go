package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"sonar/internal/download"
)

var audioExt = map[string]string{
	"audio/flac":   "flac",
	"audio/x-flac": "flac",
	"audio/mpeg":   "mp3",
	"audio/mp3":    "mp3",
	"audio/mp4":    "m4a",
	"audio/aac":    "m4a",
	"audio/x-m4a":  "m4a",
	"audio/m4a":    "m4a",
}

// Save streams an audio response to pathNoExt plus the extension its headers give, and returns that path.
// It does not close resp.Body.
func Save(resp *http.Response, pathNoExt string) (string, error) {
	ct := resp.Header.Get("Content-Type")
	mt, _, _ := mime.ParseMediaType(ct)
	ext := audioExt[mt]
	switch {
	case ext != "":
	case strings.HasPrefix(mt, "audio/"):
		ext = strings.TrimPrefix(mt, "audio/")
	case mt == "application/octet-stream":
		_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
		if e := strings.ToLower(filepath.Ext(params["filename"])); e == ".flac" || e == ".mp3" || e == ".m4a" {
			ext = e[1:]
		}
	}
	if ext == "" {
		if mt == "application/zip" {
			return "", errors.New("got an archive instead of one track")
		}
		// The body (often an HTML page) stays out of the user-facing error.
		return "", fmt.Errorf("%w: got %s instead of audio", ErrUnavailable, ct)
	}

	final := pathNoExt + "." + ext
	part := final + ".part"
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(part)
	if err != nil {
		return "", err
	}
	var w io.Writer = f
	if resp.Request != nil {
		w = io.MultiWriter(f, &progress{ctx: resp.Request.Context(), total: max(resp.ContentLength, 0)})
	}
	_, err = io.Copy(w, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(part, final)
	}
	if err != nil {
		os.Remove(part)
		return "", err
	}
	return final, nil
}

// progress reports the bytes written so far to the download queue.
type progress struct {
	ctx         context.Context
	done, total int64
}

func (p *progress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	download.Report(p.ctx, p.done, p.total)
	return len(b), nil
}
