package plugin

import (
	"path/filepath"
	"strings"
	"testing"
)

// put writes a plugin folder and returns its path.
func put(t *testing.T, root, id, version string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	writeFile(t, filepath.Join(dir, "plugin.json"),
		`{"id":"`+id+`","name":"`+id+`","version":"`+version+`","kind":"tool","run":["a.exe"]}`)
	writeFile(t, filepath.Join(dir, "a.exe"), "bin-"+version)
	return dir
}

func TestDiscoverWinnerAndBuiltin(t *testing.T) {
	root := t.TempDir()
	bdir, udir := filepath.Join(root, "b"), filepath.Join(root, "u")
	bcopy := put(t, bdir, "lucida", "1.0.0")
	sum, err := Checksum(bcopy)
	if err != nil {
		t.Fatal(err)
	}
	sums := map[string]string{"lucida": sum}

	// builtin only, matching sum
	got := Discover(bdir, udir, sums)
	if len(got) != 1 || !got[0].Builtin || got[0].Err != "" {
		t.Fatalf("builtin only: %+v", got)
	}
	// builtin only, wrong sum
	got = Discover(bdir, udir, map[string]string{"lucida": "00"})
	if len(got) != 1 || got[0].Builtin {
		t.Fatalf("bad sum: %+v", got)
	}

	// newer user copy wins
	ucopy := put(t, udir, "lucida", "1.1.0")
	got = Discover(bdir, udir, sums)
	if len(got) != 1 {
		t.Fatalf("want 1, got %+v", got)
	}
	w := got[0]
	if w.M.Version != "1.1.0" || w.Dir != ucopy || w.Builtin {
		t.Errorf("winner: %+v", w)
	}
	if len(w.Shadowed) != 1 || w.Shadowed[0] != bcopy+" (1.0.0)" {
		t.Errorf("shadowed = %v", w.Shadowed)
	}

	// equal versions: user wins
	put(t, udir, "lucida", "1.0.0")
	got = Discover(bdir, udir, sums)
	if got[0].Dir != ucopy || got[0].Builtin || len(got[0].Shadowed) != 1 {
		t.Errorf("equal versions: %+v", got[0])
	}
}

func TestDiscoverBuiltinOnlyFromBuiltinDir(t *testing.T) {
	root := t.TempDir()
	udir := filepath.Join(root, "u")
	dir := put(t, udir, "lucida", "1.0.0")
	sum, _ := Checksum(dir)
	got := Discover(filepath.Join(root, "b"), udir, map[string]string{"lucida": sum})
	if len(got) != 1 || got[0].Builtin {
		t.Errorf("user copy flagged builtin: %+v", got)
	}
}

func TestDiscoverBadManifest(t *testing.T) {
	root := t.TempDir()
	udir := filepath.Join(root, "u")
	writeFile(t, filepath.Join(udir, "broken", "plugin.json"), "{nope")
	got := Discover(filepath.Join(root, "missing"), udir, nil)
	if len(got) != 1 || got[0].M.ID != "broken" || got[0].Err == "" {
		t.Errorf("bad manifest: %+v", got)
	}
	if got := Discover(filepath.Join(root, "x"), filepath.Join(root, "y"), nil); len(got) != 0 {
		t.Errorf("missing dirs: %+v", got)
	}
}

func TestDiscoverContractMismatch(t *testing.T) {
	udir := filepath.Join(t.TempDir(), "u")
	writeFile(t, filepath.Join(udir, "new", "plugin.json"),
		`{"id":"new","name":"N","version":"1.0.0","kind":"tool","run":["a"],"contract":2}`)
	got := Discover("", udir, nil)
	if len(got) != 1 || !strings.Contains(got[0].Err, "contract 2") {
		t.Errorf("contract: %+v", got)
	}
}

func TestDiscoverUnicodeDir(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Михаил Иванов")
	bdir, udir := filepath.Join(root, "b"), filepath.Join(root, "u")
	put(t, bdir, "aaa", "1.0.0")
	put(t, udir, "bbb", "1.0.0")
	got := Discover(bdir, udir, nil)
	if len(got) != 2 || got[0].M.ID != "aaa" || got[1].M.ID != "bbb" {
		t.Errorf("unicode dirs: %+v", got)
	}
}
