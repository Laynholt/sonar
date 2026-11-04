package plugin

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"context"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func manifestJSON(id, version string) string {
	return `{"id":"` + id + `","name":"` + id + `","version":"` + version + `","kind":"tool","run":["a.exe"]}`
}

// mkZip writes a zip of name, content pairs (a name ending in "/" is a folder) and returns its path.
func mkZip(t *testing.T, pairs ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.zip")
	out, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(out)
	for i := 0; i < len(pairs); i += 2 {
		w, err := zw.Create(pairs[i])
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(pairs[i+1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func names(es []Entry) string {
	var s []string
	for _, e := range es {
		s = append(s, e.M.ID)
	}
	return strings.Join(s, ",")
}

func TestInstallZip(t *testing.T) {
	e := newEnv(t)
	r := e.open(t)
	flat := mkZip(t, "plugin.json", manifestJSON("flat", "1.0.0"), "a.exe", "bin", "lib/data.txt", "d")
	id, err := r.InstallZip(flat, false)
	if err != nil || id != "flat" {
		t.Fatalf("flat: %q, %v", id, err)
	}
	if got := readFile(t, filepath.Join(e.user, "flat", "lib", "data.txt")); got != "d" {
		t.Fatalf("data.txt: %q", got)
	}
	if g, ok := r.Get("flat"); !ok || g.Err != "" || g.Sum == "" {
		t.Fatalf("Refresh did not see it: %+v", g)
	}

	// one top folder, named otherwise: the folder is named after the manifest id
	nested := mkZip(t, "whatever/", "", "whatever/plugin.json", manifestJSON("nested", "1.0.0"), "whatever/a.exe", "bin")
	if id, err = r.InstallZip(nested, false); err != nil || id != "nested" {
		t.Fatalf("nested: %q, %v", id, err)
	}
	if got := readFile(t, filepath.Join(e.user, "nested", "a.exe")); got != "bin" {
		t.Fatalf("a.exe: %q", got)
	}
	if _, ok := r.Get("nested"); !ok {
		t.Fatal("nested not found")
	}
	if left, _ := os.ReadDir(e.user); len(left) != 2 {
		t.Fatalf("user dir holds %v", left)
	}
}

func TestInstallRejects(t *testing.T) {
	good := manifestJSON("ok", "1.0.0")
	big := strings.Repeat("x", 100)
	cases := []struct {
		name string
		zip  []string
		cap  int64
	}{
		{"zip slip", []string{"plugin.json", good, "../x", "no"}, 0},
		{"zip slip in folder", []string{"d/plugin.json", good, "d/../../x", "no"}, 0},
		{"absolute path", []string{"plugin.json", good, "/abs.txt", "no"}, 0},
		{"drive path", []string{"plugin.json", good, "C:/abs.txt", "no"}, 0},
		{"backslash", []string{"plugin.json", good, `a\b.txt`, "no"}, 0},
		{"duplicate by case", []string{"plugin.json", good, "A.txt", "1", "a.txt", "2"}, 0},
		{"too big", []string{"plugin.json", good, "a.exe", big}, 50},
		{"no manifest", []string{"a.exe", "bin"}, 0},
		{"manifest in two folders", []string{"a/plugin.json", good, "b/plugin.json", good}, 0},
		{"invalid manifest", []string{"plugin.json", `{"id":"Bad Id"}`}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.cap > 0 {
				old := maxInstallSize
				maxInstallSize = c.cap
				t.Cleanup(func() { maxInstallSize = old })
			}
			e := newEnv(t)
			r := e.open(t)
			if _, err := r.InstallZip(mkZip(t, c.zip...), false); err == nil {
				t.Fatal("installed")
			}
			if left, _ := os.ReadDir(e.user); len(left) != 0 {
				t.Fatalf("left behind: %v", left)
			}
		})
	}
}

func TestInstallLyingHeader(t *testing.T) {
	// the header says 1 byte, the data is 1000
	data := bytes.Repeat([]byte("z"), 1000)
	var packed bytes.Buffer
	fw, _ := flate.NewWriter(&packed, flate.DefaultCompression)
	fw.Write(data)
	fw.Close()
	p := filepath.Join(t.TempDir(), "lie.zip")
	out, _ := os.Create(p)
	zw := zip.NewWriter(out)
	w, _ := zw.Create("plugin.json")
	w.Write([]byte(manifestJSON("lie", "1.0.0")))
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "a.exe", Method: zip.Deflate, CRC32: crc32.ChecksumIEEE(data),
		CompressedSize64: uint64(packed.Len()), UncompressedSize64: 1})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(packed.Bytes())
	zw.Close()
	out.Close()

	e := newEnv(t)
	if _, err := e.open(t).InstallZip(p, false); err == nil {
		t.Fatal("installed")
	}
	if left, _ := os.ReadDir(e.user); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

func TestInstallExists(t *testing.T) {
	e := newEnv(t)
	r := e.open(t)
	v1 := mkZip(t, "plugin.json", manifestJSON("p", "1.0.0"), "a.exe", "one", "old.txt", "x")
	v2 := mkZip(t, "plugin.json", manifestJSON("p", "2.0.0"), "a.exe", "two")
	if _, err := r.InstallZip(v1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.InstallZip(v2, false); err != ErrExists {
		t.Fatalf("got %v", err)
	}
	if got := readFile(t, filepath.Join(e.user, "p", "a.exe")); got != "one" {
		t.Fatalf("old copy changed: %q", got)
	}
	// a broken replacement leaves the old copy alone
	if _, err := r.InstallZip(mkZip(t, "plugin.json", manifestJSON("p", "3.0.0"), "../x", "no"), true); err == nil {
		t.Fatal("installed a bad zip")
	}
	if got := readFile(t, filepath.Join(e.user, "p", "a.exe")); got != "one" {
		t.Fatalf("old copy changed by a failed replace: %q", got)
	}
	if _, err := r.InstallZip(v2, true); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(e.user, "p", "a.exe")); got != "two" {
		t.Fatalf("not replaced: %q", got)
	}
	if _, err := os.Stat(filepath.Join(e.user, "p", "old.txt")); err == nil {
		t.Fatal("old file survived the replace")
	}
	if g, _ := r.Get("p"); g.M.Version != "2.0.0" {
		t.Fatalf("registry sees %s", g.M.Version)
	}
	if left, _ := os.ReadDir(e.user); len(left) != 1 {
		t.Fatalf("user dir holds %v", left)
	}
}

// A running plugin's exe cannot be replaced on Windows until its host is closed.
func TestInstallReplaceRunning(t *testing.T) {
	e := newEnv(t)
	putFake(t, e.user, "fk", "1.0.0")
	r := e.open(t)
	if err := r.SetEnabled("fk", true, true); err != nil {
		t.Fatal(err)
	}
	h, err := r.Host("fk")
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Call(context.Background(), "initSettings", nil, new(map[string]any), nil); err != nil {
		t.Fatal(err)
	}
	var pairs []string
	entries, _ := os.ReadDir(fakeDir)
	for _, en := range entries {
		content := readFile(t, filepath.Join(fakeDir, en.Name()))
		if en.Name() == "plugin.json" {
			content = `{"id":"fk","name":"fk","version":"2.0.0","kind":"tool","run":["fake"]}`
		}
		pairs = append(pairs, en.Name(), content)
	}
	if _, err := r.InstallZip(mkZip(t, pairs...), true); err != nil {
		t.Fatal(err)
	}
	if g, _ := r.Get("fk"); g.M.Version != "2.0.0" || g.Trusted {
		t.Fatalf("after replace: %+v", g)
	}
	if left, _ := os.ReadDir(e.user); len(left) != 1 {
		t.Fatalf("user dir holds %v", left)
	}
}

func TestRemove(t *testing.T) {
	e := newEnv(t)
	e.bless(t, "both", putSettings(t, e.builtin, "both", "1.0.0", `[]`))
	e.bless(t, "bi", putSettings(t, e.builtin, "bi", "1.0.0", `[]`))
	putSettings(t, e.user, "both", "2.0.0", `[]`)
	putSettings(t, e.user, "mine", "1.0.0", `[]`)
	r := e.open(t)
	if g, _ := r.Get("both"); g.M.Version != "2.0.0" || g.Builtin {
		t.Fatalf("setup: %+v", g)
	}
	if err := r.Remove("mine"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(e.user, "mine")); err == nil {
		t.Fatal("folder still there")
	}
	if _, ok := r.Get("mine"); ok {
		t.Fatal("still listed")
	}
	if err := r.Remove("both"); err != nil {
		t.Fatal(err)
	}
	if g, ok := r.Get("both"); !ok || g.M.Version != "1.0.0" || !g.Builtin {
		t.Fatalf("built-in did not come back: %+v", g)
	}
	if err := r.Remove("bi"); err == nil || !strings.Contains(err.Error(), "built-in plugins can only be disabled") {
		t.Fatalf("got %v", err)
	}
	if err := r.Remove("nope"); err == nil {
		t.Fatal("removed nothing without an error")
	}
	if err := r.Remove(`..\x`); err == nil {
		t.Fatal("odd id")
	}
	if left, _ := os.ReadDir(e.user); len(left) != 0 {
		t.Fatalf("user dir holds %v", left)
	}
}

func zipFiles(t *testing.T, p string) []string {
	t.Helper()
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var out []string
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, "/") {
			out = append(out, f.Name)
		}
	}
	slices.Sort(out)
	return out
}

func TestExportImportRoundTrip(t *testing.T) {
	src := newEnv(t)
	src.bless(t, "bi", putSettings(t, src.builtin, "bi", "1.0.0", `[]`))
	putSettings(t, src.builtin, "dropped", "1.0.0", `[]`) // third-party copy next to sonar.exe
	putSettings(t, src.user, "mine", "1.0.0", `[{"key":"k","type":"text"}]`)
	writeFile(t, filepath.Join(src.user, "mine", "sub", "f.txt"), "f")
	writeFile(t, filepath.Join(src.data, "plugins-data", "mine", "secret"), "s")
	r := src.open(t)
	if err := r.SetEnabled("mine", true, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetEnabled("bi", false, false); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSettings("mine", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetOrder([]string{"mine", "bi", "dropped"}); err != nil {
		t.Fatal(err)
	}
	s := r.State()
	s.Migrated = true
	if err := r.SetState(s); err != nil {
		t.Fatal(err)
	}

	zp := filepath.Join(t.TempDir(), "out.zip")
	if err := r.Export(zp); err != nil {
		t.Fatal(err)
	}
	want := []string{"plugins/dropped/a.exe", "plugins/dropped/plugin.json", "plugins/mine/a.exe", "plugins/mine/plugin.json",
		"plugins/mine/sub/f.txt", "state.json"}
	if got := zipFiles(t, zp); !reflect.DeepEqual(got, want) {
		t.Fatalf("zip holds %v, want %v", got, want)
	}
	zr, _ := zip.OpenReader(zp)
	var raw map[string]json.RawMessage
	for _, f := range zr.File {
		if f.Name == "state.json" {
			rc, _ := f.Open()
			if err := json.NewDecoder(rc).Decode(&raw); err != nil {
				t.Fatal(err)
			}
			rc.Close()
		}
	}
	zr.Close()
	for _, k := range []string{"migrated", "trusted"} {
		if _, has := raw[k]; has {
			t.Fatalf("state.json has %q", k)
		}
	}

	// into a fresh registry that already has the built-in and its own state
	dst := newEnv(t)
	dst.bless(t, "bi", putSettings(t, dst.builtin, "bi", "1.0.0", `[]`))
	putSettings(t, dst.user, "keep", "1.0.0", `[]`)
	d := dst.open(t)
	if err := d.SetEnabled("keep", true, true); err != nil {
		t.Fatal(err)
	}
	installed, needTrust, err := d.Import(zp, "")
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(installed)
	slices.Sort(needTrust)
	// dropped was off in the export, so trusting the import must not switch it on
	if !reflect.DeepEqual(installed, []string{"dropped", "mine"}) || !reflect.DeepEqual(needTrust, []string{"mine"}) {
		t.Fatalf("installed %v, needTrust %v", installed, needTrust)
	}
	if got := readFile(t, filepath.Join(dst.user, "mine", "sub", "f.txt")); got != "f" {
		t.Fatalf("f.txt: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst.data, "plugins-data")); err == nil {
		t.Fatal("plugins-data appeared")
	}
	st := d.State()
	if !st.Enabled["mine"] || st.Enabled["bi"] || !st.Enabled["keep"] || st.Settings["mine"]["k"] != "v" || st.Migrated || len(st.Trusted) != 1 {
		t.Fatalf("state: %+v", st)
	}
	if got := names(d.List()); got != "mine,bi,dropped,keep" {
		t.Fatalf("order: %s", got)
	}
	if m, _ := d.Get("mine"); m.Enabled || m.Trusted {
		t.Fatalf("imported plugin runs before trust: %+v", m)
	}
	if err := d.SetEnabled("mine", true, true); err != nil {
		t.Fatal(err)
	}
	if m, _ := d.Get("mine"); !m.Enabled {
		t.Fatalf("not on after confirm: %+v", m)
	}
}

func TestImportConflict(t *testing.T) {
	src := newEnv(t)
	putSettings(t, src.user, "a", "2.0.0", `[]`)
	putSettings(t, src.user, "b", "1.0.0", `[]`)
	zp := filepath.Join(t.TempDir(), "x.zip")
	if err := src.open(t).Export(zp); err != nil {
		t.Fatal(err)
	}

	dst := newEnv(t)
	putSettings(t, dst.user, "a", "1.0.0", `[]`)
	d := dst.open(t)
	_, _, err := d.Import(zp, "")
	var c *Conflict
	if !errors.As(err, &c) || !reflect.DeepEqual(c.IDs, []string{"a"}) {
		t.Fatalf("got %v", err)
	}
	if _, ok := d.Get("b"); ok {
		t.Fatal("a conflicting import installed something")
	}

	installed, _, err := d.Import(zp, "skip")
	if err != nil || !reflect.DeepEqual(installed, []string{"b"}) {
		t.Fatalf("skip: %v, %v", installed, err)
	}
	if g, _ := d.Get("a"); g.M.Version != "1.0.0" {
		t.Fatalf("skip replaced a: %s", g.M.Version)
	}
	installed, _, err = d.Import(zp, "replace")
	slices.Sort(installed)
	if err != nil || !reflect.DeepEqual(installed, []string{"a", "b"}) {
		t.Fatalf("replace: %v, %v", installed, err)
	}
	if g, _ := d.Get("a"); g.M.Version != "2.0.0" {
		t.Fatalf("replace kept a: %s", g.M.Version)
	}
	if _, _, err := d.Import(zp, "bogus"); err == nil {
		t.Fatal("odd choice accepted")
	}
}

func TestImportRejects(t *testing.T) {
	e := newEnv(t)
	d := e.open(t)
	good := manifestJSON("ok", "1.0.0")
	for name, z := range map[string][]string{
		"slip":        {"plugins/ok/plugin.json", good, "plugins/ok/../../x", "no"},
		"no manifest": {"plugins/ok/a.exe", "bin"},
		"bad state":   {"plugins/ok/plugin.json", good, "state.json", "{nope"},
	} {
		if _, _, err := d.Import(mkZip(t, z...), ""); err == nil {
			t.Fatalf("%s: imported", name)
		}
	}
	if left, _ := os.ReadDir(e.user); len(left) != 0 {
		t.Fatalf("left behind: %v", left)
	}
}

func TestImportSkipKeepsState(t *testing.T) {
	src := newEnv(t)
	putSettings(t, src.user, "a", "2.0.0", `[]`)
	putSettings(t, src.user, "b", "1.0.0", `[]`)
	s := src.open(t)
	for _, id := range []string{"a", "b"} {
		if err := s.SetEnabled(id, true, true); err != nil {
			t.Fatal(err)
		}
		if err := s.SetSettings(id, map[string]any{"k": id + "-imported"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetOrder([]string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	zp := filepath.Join(t.TempDir(), "x.zip")
	if err := s.Export(zp); err != nil {
		t.Fatal(err)
	}

	dst := newEnv(t)
	putSettings(t, dst.user, "a", "1.0.0", `[]`)
	d := dst.open(t)
	if err := d.SetEnabled("a", true, true); err != nil {
		t.Fatal(err)
	}
	if err := d.SetEnabled("a", false, false); err != nil {
		t.Fatal(err)
	}
	if err := d.SetSettings("a", map[string]any{"k": "mine"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Import(zp, "skip"); err != nil {
		t.Fatal(err)
	}
	st := d.State()
	if st.Enabled["a"] || !reflect.DeepEqual(st.Settings["a"], map[string]any{"k": "mine"}) {
		t.Fatalf("skipped plugin's state was overwritten: %+v", st)
	}
	if !st.Enabled["b"] || st.Settings["b"]["k"] != "b-imported" {
		t.Fatalf("the installed plugin's state was not applied: %+v", st)
	}
	if got := names(d.List()); got != "b,a" {
		t.Fatalf("order: %s", got)
	}
}

// Folder names don't matter: a folder whose manifest says foo is the installed foo.
func TestInstallFindsCopyByManifestID(t *testing.T) {
	e := newEnv(t)
	for _, dir := range []string{"foo-old", "foo-bak"} {
		writeFile(t, filepath.Join(e.user, dir, "plugin.json"), manifestJSON("foo", "1.0.0"))
		writeFile(t, filepath.Join(e.user, dir, "a.exe"), "old")
	}
	r := e.open(t)
	v2 := mkZip(t, "plugin.json", manifestJSON("foo", "2.0.0"), "a.exe", "new")
	if _, err := r.InstallZip(v2, false); err != ErrExists {
		t.Fatalf("got %v", err)
	}
	if _, _, err := r.Import(mkZip(t, "plugins/foo/plugin.json", manifestJSON("foo", "2.0.0")), ""); err == nil {
		t.Fatal("import did not see the clash")
	} else if c := new(Conflict); !errors.As(err, &c) || !reflect.DeepEqual(c.IDs, []string{"foo"}) {
		t.Fatalf("got %v", err)
	}
	if _, err := r.InstallZip(v2, true); err != nil {
		t.Fatal(err)
	}
	left, _ := os.ReadDir(e.user)
	if len(left) != 1 {
		t.Fatalf("user dir holds %v", left)
	}
	if got := readFile(t, filepath.Join(e.user, left[0].Name(), "a.exe")); got != "new" {
		t.Fatalf("%s holds %q", left[0].Name(), got)
	}
	if g, _ := r.Get("foo"); g.M.Version != "2.0.0" {
		t.Fatalf("registry sees %s", g.M.Version)
	}
}

func TestRefreshSweepsStaleTemps(t *testing.T) {
	e := newEnv(t)
	putSettings(t, e.user, "keep", "1.0.0", `[]`)
	for _, d := range []string{".install-123", ".old-keep", ".mine"} {
		writeFile(t, filepath.Join(e.user, d, "f"), "x")
	}
	r := e.open(t)
	r.Refresh()
	var got []string
	entries, _ := os.ReadDir(e.user)
	for _, en := range entries {
		got = append(got, en.Name())
	}
	if !reflect.DeepEqual(got, []string{".mine", "keep"}) {
		t.Fatalf("user dir holds %v", got)
	}
	if names(r.List()) != "keep" {
		t.Fatalf("list: %s", names(r.List()))
	}
}

// A hand-made export can't grant trust or touch ids that aren't there.
func TestImportStateGrantsNothing(t *testing.T) {
	e := newEnv(t)
	d := e.open(t)
	state := `{"enabled":{"mine":true,"ghost":true},"order":["ghost","mine"],"settings":{"ghost":{"a":1},"mine":{"k":"v"}},
		"trusted":{"mine":"deadbeef","ghost":"x"},"migrated":true}`
	installed, needTrust, err := d.Import(mkZip(t, "plugins/mine/plugin.json", manifestJSON("mine", "1.0.0"), "state.json", state), "")
	if err != nil || !reflect.DeepEqual(installed, []string{"mine"}) || !reflect.DeepEqual(needTrust, []string{"mine"}) {
		t.Fatalf("%v, %v, %v", installed, needTrust, err)
	}
	st := d.State()
	if len(st.Trusted) != 0 || st.Migrated {
		t.Fatalf("trust or migration came in: %+v", st)
	}
	if _, ok := st.Enabled["ghost"]; ok || st.Settings["ghost"] != nil || slices.Contains(st.Order, "ghost") {
		t.Fatalf("unknown id came in: %+v", st)
	}
	if m, _ := d.Get("mine"); m.Enabled || m.Trusted || m.Values["k"] != "v" {
		t.Fatalf("mine: %+v", m)
	}
}

func TestRestoreBuiltin(t *testing.T) {
	e := newEnv(t)
	good := putFake(t, e.builtin, "good", "1.0.0") // intact: left alone
	e.bless(t, "good", good)
	gone := putFake(t, e.builtin, "gone", "1.0.0")
	e.bless(t, "gone", gone)
	want := map[string]string{}
	filepath.WalkDir(gone, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(gone, p)
			want["gone/"+filepath.ToSlash(rel)] = readFile(t, p)
		}
		return nil
	})
	os.RemoveAll(gone)
	broken := putFake(t, e.builtin, "broken", "1.0.0")
	e.bless(t, "broken", broken)
	os.WriteFile(filepath.Join(broken, "plugin.json"), []byte("{}"), 0o644)
	e.sums["absent"] = "00" // the zip lacks it
	r := e.open(t)

	var pairs []string
	for name, body := range want {
		pairs = append(pairs, name, body)
	}
	pairs = append(pairs, "broken/plugin.json", "tampered") // a wrong sum must not be swapped in
	restored, err := r.RestoreBuiltin(mkZip(t, pairs...))
	if !slices.Equal(restored, []string{"gone"}) {
		t.Fatalf("restored %v", restored)
	}
	if err == nil || !strings.Contains(err.Error(), "absent: not in the archive") || !strings.Contains(err.Error(), "broken: the download does not match") {
		t.Fatalf("err: %v", err)
	}
	if g, ok := r.Get("gone"); !ok || !g.Builtin {
		t.Fatalf("gone after restore: %+v", g)
	}
	if got := readFile(t, filepath.Join(broken, "plugin.json")); got != "{}" {
		t.Fatalf("broken was replaced by a tampered copy: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(e.builtin, ".*")); len(left) != 0 {
		t.Fatalf("temps left: %v", left)
	}
}
