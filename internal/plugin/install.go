package plugin

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"sonar/internal/ffinstall"
)

// maxInstallSize caps the total uncompressed size of a zip; tests shorten it.
var maxInstallSize = int64(300 << 20)

// ErrExists is what InstallZip answers when the zip's id is already installed in userDir and replace is off.
var ErrExists = errors.New("exists")

// Conflict is what Import answers when ids are already installed and no choice was given.
type Conflict struct{ IDs []string }

func (c *Conflict) Error() string { return "already installed: " + strings.Join(c.IDs, ", ") }

var errTooBig = errors.New("zip is too big once unpacked")

// member is one file of a zip, named relative to the plugin folder.
type member struct {
	rel string
	f   *zip.File
}

// pkg is one plugin folder inside a zip.
type pkg struct {
	m     Manifest
	files []member
}

// checkZip refuses unsafe or clashing names, non-regular entries and an unpacked size over the cap.
func checkZip(zr *zip.ReadCloser) error {
	seen := map[string]bool{}
	var total uint64
	for _, f := range zr.File {
		name := strings.TrimSuffix(f.Name, "/")
		if err := ffinstall.SafeName(name); err != nil {
			return err
		}
		if key := strings.ToLower(name); seen[key] { // Windows names are case-insensitive
			return fmt.Errorf("duplicate file name in archive: %q", f.Name)
		} else {
			seen[key] = true
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return fmt.Errorf("archive entry %q is not a plain file", f.Name)
		}
		if total += f.UncompressedSize64; total > uint64(maxInstallSize) {
			return errTooBig
		}
	}
	return nil
}

// members lists the files under prefix ("" or "dir/"), named relative to it.
func members(zr *zip.ReadCloser, prefix string) (out []member) {
	for _, f := range zr.File {
		if rel, ok := strings.CutPrefix(f.Name, prefix); ok && rel != "" && !f.FileInfo().IsDir() {
			out = append(out, member{rel, f})
		}
	}
	return out
}

// newPkg reads plugin.json from files.
func newPkg(files []member) (pkg, error) {
	for _, m := range files {
		if m.rel != "plugin.json" {
			continue
		}
		rc, err := m.f.Open()
		if err != nil {
			return pkg{}, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 1<<20+1))
		rc.Close()
		if err != nil {
			return pkg{}, err
		}
		if len(raw) > 1<<20 {
			return pkg{}, errors.New("plugin.json is over 1 MB")
		}
		man, err := ParseManifest(raw)
		if err != nil {
			return pkg{}, err
		}
		return pkg{man, files}, nil
	}
	return pkg{}, errors.New("no plugin.json in the archive")
}

// rootPkg is the plugin of a zip with plugin.json at the root or inside its one top folder.
func rootPkg(zr *zip.ReadCloser) (pkg, error) {
	prefix := ""
	if !slices.ContainsFunc(zr.File, func(f *zip.File) bool { return f.Name == "plugin.json" }) {
		tops := map[string]bool{}
		for _, f := range zr.File {
			top, _, _ := strings.Cut(f.Name, "/")
			tops[top] = true
		}
		if len(tops) == 1 {
			for top := range tops {
				prefix = top + "/"
			}
		}
	}
	return newPkg(members(zr, prefix))
}

// InstallZip unpacks a plugin zip to userDir/<id> and returns the id from its manifest.
// Without replace an installed id gives ErrExists.
func (r *Registry) InstallZip(zipPath string, replace bool) (id string, err error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", err
	}
	defer zr.Close()
	if err := checkZip(zr); err != nil {
		return "", err
	}
	p, err := rootPkg(zr)
	if err != nil {
		return "", err
	}
	budget := maxInstallSize
	r.installMu.Lock()
	err = r.install(p, replace, &budget)
	r.installMu.Unlock()
	if err != nil {
		return "", err
	}
	r.Refresh()
	return p.m.ID, nil
}

// install unpacks p next to its final place, then swaps it in. budget is what the zip may still unpack.
func (r *Registry) install(p pkg, replace bool, budget *int64) error {
	copies := r.userCopies(p.m.ID)
	if len(copies) > 0 && !replace {
		return ErrExists
	}
	dest := filepath.Join(r.userDir, p.m.ID)
	exists := len(copies) > 0
	if exists && !slices.Contains(copies, dest) {
		dest = copies[0] // a folder named otherwise holds this id: replace that one
	}
	if err := os.MkdirAll(r.userDir, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(r.userDir, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp) // nothing is left of it after the rename
	for _, m := range p.files {
		if err := extract(m.f, filepath.Join(tmp, filepath.FromSlash(m.rel)), budget); err != nil {
			return err
		}
	}
	if !exists {
		return os.Rename(tmp, dest)
	}
	old, err := r.moveAside(dest)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Rename(old, dest)
		return err
	}
	if err := os.RemoveAll(old); err != nil {
		r.logf("%s: %v", old, err)
	}
	for _, c := range copies { // more copies of the id would only fight over the tie
		if c != dest {
			if err := r.discard(c); err != nil {
				r.logf("%s: %v", c, err)
			}
		}
	}
	return nil
}

// userCopies lists the userDir folders whose manifest id is id; folder names don't matter (Discover goes by id).
func (r *Registry) userCopies(id string) (dirs []string) {
	entries, _ := os.ReadDir(r.userDir)
	for _, e := range entries {
		if d := filepath.Join(r.userDir, e.Name()); e.IsDir() && !strings.HasPrefix(e.Name(), ".") && load(d, e.Name(), false, nil).M.ID == id {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// discard moves dir aside and deletes it. If only the delete fails the plugin is gone anyway and the next sweep retries.
func (r *Registry) discard(dir string) error {
	old, err := r.moveAside(dir)
	if err == nil {
		if rerr := os.RemoveAll(old); rerr != nil {
			r.logf("%s: %v", old, rerr)
		}
	}
	return err
}

// sweepStale deletes what a killed install left in userDir. Call with installMu held.
func (r *Registry) sweepStale() {
	entries, _ := os.ReadDir(r.userDir)
	for _, e := range entries {
		if n := e.Name(); e.IsDir() && (strings.HasPrefix(n, ".install-") || strings.HasPrefix(n, ".old-")) {
			if err := os.RemoveAll(filepath.Join(r.userDir, n)); err != nil {
				r.logf("%s: %v", n, err)
			}
		}
	}
}

// moveAside renames dir to a hidden sibling, so a delete that fails half-way can't leave a broken plugin.
// A running plugin's exe is locked on Windows, so its host is closed first.
func (r *Registry) moveAside(dir string) (string, error) {
	r.mu.Lock()
	var stop []*Host
	for id, rec := range r.hosts {
		if filepath.Clean(rec.dir) == filepath.Clean(dir) {
			stop = append(stop, rec.h)
			delete(r.hosts, id)
		}
	}
	r.mu.Unlock()
	closeAll(stop)
	old := filepath.Join(filepath.Dir(dir), ".old-"+filepath.Base(dir))
	os.RemoveAll(old) // left by an earlier failed cleanup
	return old, os.Rename(dir, old)
}

// extract copies at most the entry's declared size, so a lying header can't inflate it, and charges budget.
func extract(f *zip.File, to string, budget *int64) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, f.Mode().Perm()|0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if *budget -= n; err == nil && (uint64(n) > f.UncompressedSize64 || *budget < 0) {
		err = fmt.Errorf("%s: %w", f.Name, errTooBig)
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// Remove deletes the userDir copy of a plugin. A built-in one can only be disabled.
func (r *Registry) Remove(id string) error {
	dirs := r.userCopies(id)
	if len(dirs) == 0 {
		r.mu.Lock()
		f, ok := r.find(id)
		r.mu.Unlock()
		if ok && f.Builtin {
			return errors.New("built-in plugins can only be disabled")
		}
		return fmt.Errorf("plugin %q: not installed in the user folder", id)
	}
	var err error
	r.installMu.Lock()
	for _, d := range dirs {
		err = errors.Join(err, r.discard(d))
	}
	r.installMu.Unlock()
	r.Refresh()
	return err
}

// portable is the part of State that travels in an export: what the user chose, not what they trusted.
type portable struct {
	Enabled  map[string]bool           `json:"enabled"`
	Order    []string                  `json:"order"`
	Settings map[string]map[string]any `json:"settings"`
}

// Export writes every userDir plugin, every non-built-in winner from builtinDir and the state to a zip:
// plugins/<id>/... and state.json. Plugin data stays out.
func (r *Registry) Export(zipPath string) (err error) {
	dirs := map[string]string{} // id → folder
	add := func(dir string) {
		raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if err != nil {
			return
		}
		if m, err := ParseManifest(raw); err == nil && dirs[m.ID] == "" {
			dirs[m.ID] = dir
		}
	}
	r.mu.Lock()
	for _, f := range r.found { // winners first: they are what runs
		if !f.Builtin && filepath.Dir(f.Dir) == filepath.Clean(r.builtinDir) {
			add(f.Dir)
		}
	}
	r.mu.Unlock()
	entries, _ := os.ReadDir(r.userDir)
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			add(filepath.Join(r.userDir, e.Name()))
		}
	}

	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(zipPath)
		}
	}()
	zw := zip.NewWriter(out)
	for _, id := range slices.Sorted(maps.Keys(dirs)) {
		root := dirs[id]
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			w, err := zw.Create("plugins/" + id + "/" + filepath.ToSlash(rel))
			if err != nil {
				return err
			}
			src, err := os.Open(p)
			if err != nil {
				return err
			}
			defer src.Close()
			_, err = io.Copy(w, src)
			return err
		})
		if err != nil {
			return err
		}
	}
	st := r.State()
	raw, err := json.MarshalIndent(portable{st.Enabled, st.Order, st.Settings}, "", "  ")
	if err != nil {
		return err
	}
	w, err := zw.Create("state.json")
	if err != nil {
		return err
	}
	if _, err := w.Write(raw); err != nil {
		return err
	}
	return zw.Close()
}

// Import installs the plugins of an export and merges its state for the ids that exist afterwards.
// choice decides ids that are already installed: "" answers *Conflict, "replace" swaps them, "skip" keeps the old copy.
// SetState never trusts, so needTrust lists every imported plugin that cannot run until the user confirms it.
func (r *Registry) Import(zipPath, choice string) (installed, needTrust []string, err error) {
	if choice != "" && choice != "replace" && choice != "skip" {
		return nil, nil, fmt.Errorf("choice %q: want replace or skip", choice)
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, err
	}
	defer zr.Close()
	if err := checkZip(zr); err != nil {
		return nil, nil, err
	}

	// Read everything before touching the disk.
	var pkgs []pkg
	folders := map[string]bool{}
	for _, f := range zr.File {
		if rest, ok := strings.CutPrefix(f.Name, "plugins/"); ok {
			if folder, _, ok := strings.Cut(rest, "/"); ok {
				folders[folder] = true
			}
		}
	}
	ids := map[string]bool{}
	for _, folder := range slices.Sorted(maps.Keys(folders)) {
		p, err := newPkg(members(zr, "plugins/"+folder+"/"))
		if err != nil {
			return nil, nil, fmt.Errorf("plugins/%s: %w", folder, err)
		}
		if ids[p.m.ID] {
			return nil, nil, fmt.Errorf("plugin %q is in the archive twice", p.m.ID)
		}
		ids[p.m.ID] = true
		pkgs = append(pkgs, p)
	}
	var in *portable
	for _, f := range zr.File {
		if f.Name != "state.json" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(rc, 4<<20))
		rc.Close()
		in = new(portable)
		if err == nil {
			err = json.Unmarshal(raw, in)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("state.json: %w", err)
		}
	}

	var clash []string
	for _, p := range pkgs {
		if len(r.userCopies(p.m.ID)) > 0 {
			clash = append(clash, p.m.ID)
		}
	}
	if len(clash) > 0 && choice == "" {
		return nil, nil, &Conflict{clash}
	}

	installed = []string{}
	budget := maxInstallSize
	r.installMu.Lock()
	for _, p := range pkgs {
		if choice == "skip" && slices.Contains(clash, p.m.ID) {
			continue
		}
		if err = r.install(p, true, &budget); err != nil {
			err = fmt.Errorf("plugin %q: %w", p.m.ID, err)
			break
		}
		installed = append(installed, p.m.ID)
	}
	r.installMu.Unlock()
	r.Refresh()
	if err != nil {
		return installed, nil, err
	}

	present := map[string]bool{}
	for _, e := range r.List() {
		present[e.M.ID] = true
	}
	if in != nil {
		if choice == "skip" { // the old copy stays, so the user's settings and on/off for it stay too
			for _, id := range clash {
				delete(in.Enabled, id)
				delete(in.Settings, id)
				in.Order = slices.DeleteFunc(in.Order, func(o string) bool { return o == id })
			}
		}
		err = r.change(func(st *State) error {
			for id, on := range in.Enabled {
				if present[id] {
					st.Enabled[id] = on
				}
			}
			for id, v := range in.Settings {
				if present[id] {
					st.Settings[id] = maps.Clone(v)
				}
			}
			var order []string // the export's order first, then whatever else we had
			for _, id := range slices.Concat(in.Order, st.Order) {
				if (present[id] || slices.Contains(st.Order, id)) && !slices.Contains(order, id) {
					order = append(order, id)
				}
			}
			st.Order = order
			return nil
		})
		if err != nil {
			return installed, nil, err
		}
	}
	needTrust = []string{}
	for _, id := range installed {
		if e, ok := r.Get(id); ok && !e.Trusted && e.Err == "" { // a broken copy can't run, so asking is pointless
			needTrust = append(needTrust, id)
		}
	}
	return installed, needTrust, nil
}
