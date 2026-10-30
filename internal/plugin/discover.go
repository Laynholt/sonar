package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Found is one plugin folder that won discovery for its id.
type Found struct {
	M        Manifest
	Dir      string
	Builtin  bool     // lives in the built-in folder and matches the sum baked into the build
	Sum      string   // checksum of Dir, computed for built-in-folder copies only
	Err      string   // why it cannot run; M.ID is then the folder name
	Shadowed []string // "dir (version)" of the losing copies
}

// BuiltinSums is "id=hex,id=hex", set at build time with
// -ldflags -X sonar/internal/plugin.BuiltinSums=...
var BuiltinSums string

func ParseSums(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		id, sum, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if ok && id != "" && sum != "" {
			out[id] = sum
		}
	}
	return out
}

// Checksum hashes every file under dir: sorted slash-separated relative paths, each followed by \0 and the bytes.
func Checksum(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		h.Write([]byte(f))
		h.Write([]byte{0})
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Discover reads one level of subfolders in each dir and returns one Found per id, sorted by id.
// The higher version wins; on a tie userDir beats builtinDir. A missing dir is fine.
func Discover(builtinDir, userDir string, sums map[string]string) []Found {
	byID := map[string]Found{}
	for _, src := range []struct {
		dir     string
		builtin bool
	}{{builtinDir, true}, {userDir, false}} { // user last, so it wins ties
		if src.dir == "" {
			continue
		}
		entries, _ := os.ReadDir(src.dir)
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") { // "." folders are installs in flight
				continue
			}
			f := load(filepath.Join(src.dir, e.Name()), e.Name(), src.builtin, sums)
			old, dup := byID[f.M.ID]
			switch {
			case !dup:
				byID[f.M.ID] = f
			case CompareVersions(f.M.Version, old.M.Version) >= 0:
				f.Shadowed = append(old.Shadowed, label(old))
				byID[f.M.ID] = f
			default:
				old.Shadowed = append(old.Shadowed, label(f))
				byID[f.M.ID] = old
			}
		}
	}
	out := make([]Found, 0, len(byID))
	for _, f := range byID {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].M.ID < out[j].M.ID })
	return out
}

func label(f Found) string { return fmt.Sprintf("%s (%s)", f.Dir, f.M.Version) }

func load(dir, name string, inBuiltinDir bool, sums map[string]string) Found {
	f := Found{Dir: dir, M: Manifest{ID: name}}
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		f.Err = err.Error()
		return f
	}
	m, err := ParseManifest(raw)
	if err != nil {
		f.Err = err.Error()
		return f
	}
	f.M = m
	if m.Contract != Contract {
		f.Err = fmt.Sprintf("needs contract %d, sonar speaks %d", m.Contract, Contract)
	}
	if inBuiltinDir {
		if want, ok := sums[m.ID]; ok {
			if f.Sum, err = Checksum(dir); err == nil {
				f.Builtin = f.Err == "" && f.Sum == want
			}
		}
	}
	return f
}
