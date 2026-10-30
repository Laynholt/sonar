package ffinstall

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	Manifest = "sonar-ffmpeg.txt"
	MaxSize  = 300 << 20
)

var ErrNotEmpty = errors.New("folder is not empty")

// staleTemp matches only the names CreateTemp/MkdirTemp give our temps (a run of digits for the *).
var staleTemp = regexp.MustCompile(`^ffmpeg-\d+(\.zip)?\.tmp$`)

// Progress.Step is "download" (bytes) or "install" (files).
type Progress struct {
	Step        string
	Done, Total int64
}

var (
	// verify runs the new binary; tests replace it.
	verify = func(ctx context.Context, ffmpegPath string) error {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ffmpegPath, "-version")
		hideWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s -version: %w: %.200s", filepath.Base(ffmpegPath), err, out)
		}
		return nil
	}
	stallTimeout = 30 * time.Second // no bytes for this long aborts the download
	sumsTimeout  = 30 * time.Second // the checksums file is tiny: a slow host is a dead host
	maxUnpacked  = int64(MaxSize * 3)

	errStalled  = errors.New("stalled")
	errTooLarge = errors.New("download too large")
)

// CanInstall is nil when dir is missing, empty, or already holds our manifest.
func CanInstall(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	for _, e := range entries {
		if e.Name() == Manifest {
			return nil
		}
	}
	return ErrNotEmpty
}

// Install downloads archive, checks it against sums, unpacks ffmpeg.exe and the
// DLLs flat into dir, verifies the result and returns the ffmpeg.exe path.
// Nothing in dir changes until everything has been downloaded, checked and verified.
func Install(ctx context.Context, c *http.Client, archive, sums Asset, dir string, report func(Progress)) (string, error) {
	if report == nil {
		report = func(Progress) {}
	}
	if archive.Size > MaxSize {
		return "", fmt.Errorf("%w: %d bytes", errTooLarge, archive.Size)
	}
	if err := CanInstall(dir); err != nil {
		return "", err
	}
	want, err := fetchSum(ctx, c, sums, archive.Name)
	if err != nil {
		return "", err
	}

	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	// A previous run killed mid-install (window closed) cannot clean up after itself.
	// ReadDir, not Glob: a picked folder like "Music [2020]" would be read as a pattern.
	entries, _ := os.ReadDir(parent)
	for _, e := range entries {
		if staleTemp.MatchString(e.Name()) {
			os.RemoveAll(filepath.Join(parent, e.Name()))
		}
	}
	zf, err := os.CreateTemp(parent, "ffmpeg-*.zip.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(zf.Name())
	got, err := download(ctx, c, archive, zf, report)
	if cerr := zf.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(got, want) {
		return "", fmt.Errorf("checksum mismatch for %s", archive.Name)
	}

	tmp, err := os.MkdirTemp(parent, "ffmpeg-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	files, err := unpack(ctx, zf.Name(), tmp, report)
	if err != nil {
		return "", err
	}
	if err := verify(ctx, filepath.Join(tmp, "ffmpeg.exe")); err != nil {
		return "", fmt.Errorf("new FFmpeg does not run: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := swap(tmp, dir, files); err != nil {
		return "", err
	}
	return filepath.Join(dir, "ffmpeg.exe"), nil
}

// fetchSum returns the sha256 hex listed for name in the checksums asset.
func fetchSum(ctx context.Context, c *http.Client, sums Asset, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, sumsTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sums.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksums: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(body), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("no checksum listed for %s", name)
}

// download streams a to w and returns the sha256 hex. It aborts when no bytes
// arrive for stallTimeout or more than MaxSize bytes come.
func download(ctx context.Context, c *http.Client, a Asset, w io.Writer, report func(Progress)) (string, error) {
	dctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	idle := stallTimeout
	watchdog := time.AfterFunc(idle, func() { cancel(errStalled) })
	defer watchdog.Stop()
	fail := func(err error) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if errors.Is(context.Cause(dctx), errStalled) {
			return "", fmt.Errorf("download stalled: no data for %s", idle)
		}
		return "", err
	}

	req, err := http.NewRequestWithContext(dctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > MaxSize {
		return "", fmt.Errorf("%w: %d bytes", errTooLarge, resp.ContentLength)
	}
	total := a.Size
	if total <= 0 {
		total = max(resp.ContentLength, 0)
	}

	h := sha256.New()
	out := io.MultiWriter(w, h)
	buf := make([]byte, 64<<10)
	var n int64
	var last time.Time
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			watchdog.Reset(idle)
			if _, err := out.Write(buf[:k]); err != nil {
				return "", err
			}
			if n += int64(k); n > MaxSize {
				return "", errTooLarge
			}
			if now := time.Now(); now.Sub(last) >= 100*time.Millisecond {
				last = now
				report(Progress{"download", n, total})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fail(rerr)
		}
	}
	report(Progress{"download", n, n})
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SafeName is nil when a zip entry name is a clean relative path that stays inside the folder it is
// unpacked into: no "..", no drive or root, no backslash (filepath treats it as a separator on Windows).
func SafeName(name string) error {
	if name == "." || strings.ContainsAny(name, `\:`) || !filepath.IsLocal(name) || path.Clean(name) != name {
		return fmt.Errorf("unsafe file name in archive: %q", name)
	}
	return nil
}

// unpack copies */bin/ffmpeg.exe and */bin/*.dll from the zip flat into dst.
func unpack(ctx context.Context, zipPath, dst string, report func(Progress)) ([]string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	var picked []*zip.File
	hasFFmpeg := false
	for _, f := range zr.File {
		exe, _ := path.Match("*/bin/ffmpeg.exe", f.Name)
		dll, _ := path.Match("*/bin/*.dll", f.Name)
		if exe || dll {
			picked = append(picked, f)
			hasFFmpeg = hasFFmpeg || exe
		}
	}
	if !hasFFmpeg {
		return nil, errors.New("archive has no bin/ffmpeg.exe")
	}

	// Validate every entry before writing anything. The basename must be a plain
	// file name: filepath treats backslashes as separators on Windows.
	var names []string
	var total uint64
	for _, f := range picked {
		name := path.Base(f.Name)
		if err := SafeName(name); err != nil {
			return nil, err
		}
		// Windows names are case-insensitive, so "A.dll" and "a.dll" are the same file.
		if slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, name) }) {
			return nil, fmt.Errorf("duplicate file name in archive: %q", name)
		}
		names = append(names, name)
		if total += f.UncompressedSize64; total > uint64(maxUnpacked) {
			return nil, fmt.Errorf("%w: unpacked size over %d bytes", errTooLarge, maxUnpacked)
		}
	}

	for i, f := range picked {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := extract(f, filepath.Join(dst, names[i])); err != nil {
			return nil, err
		}
		report(Progress{"install", int64(i + 1), int64(len(picked))})
	}
	return names, nil
}

// extract copies at most the entry's declared size, so a lying header can't inflate it.
func extract(f *zip.File, to string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if err == nil && uint64(n) > f.UncompressedSize64 {
		err = fmt.Errorf("%w: %s is bigger than its header says", errTooLarge, f.Name)
	}
	if err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// swap removes the files the old manifest lists, moves the new ones in from tmp
// and writes the manifest last, so a half-done swap never looks complete.
func swap(tmp, dir string, files []string) error {
	if err := CanInstall(dir); err != nil {
		return err
	}
	_, statErr := os.Stat(dir)
	created := errors.Is(statErr, os.ErrNotExist)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	err := func() error {
		if old, err := os.ReadFile(filepath.Join(dir, Manifest)); err == nil {
			for _, name := range strings.Fields(string(old)) {
				if name == Manifest || !filepath.IsLocal(name) || filepath.Base(name) != name {
					continue
				}
				if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
		for _, name := range files {
			if err := os.Rename(filepath.Join(tmp, name), filepath.Join(dir, name)); err != nil {
				return err
			}
		}
		return os.WriteFile(filepath.Join(dir, Manifest), []byte(strings.Join(files, "\n")+"\n"), 0o644)
	}()
	if err != nil && created {
		os.RemoveAll(dir)
	}
	return err
}
