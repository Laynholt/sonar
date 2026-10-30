package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseManifestValid(t *testing.T) {
	cases := map[string]string{
		"source": `{"id":"lucida","name":"Lucida","version":"1.0.0","kind":"source","run":["lucida.exe"],
			"source":{"service":"lucida","label":"Lucida","links":["^https://x"],"can":["find","tracks"]}}`,
		"downloader": `{"id":"dd","name":"DD","version":"1.0.0","kind":"downloader","run":["dd.exe"],
			"downloader":{"accepts":"any","guard":{"limit":50,"waits":[15,30],"wait":30}}}`,
		"tagger": `{"id":"chosic","name":"Chosic","version":"1.0.0","kind":"tagger","run":["c.exe"],
			"tagger":{"fields":["genre"],"needs":"spotify"}}`,
		"tool": `{"id":"tool-1","name":"T","version":"0.1.0","kind":"tool","run":["t.exe"]}`,
	}
	for kind, raw := range cases {
		m, err := ParseManifest([]byte(raw))
		if err != nil {
			t.Fatalf("%s: %v", kind, err)
		}
		if string(m.Kind) != kind {
			t.Errorf("%s: kind %q", kind, m.Kind)
		}
		if m.Contract != 1 {
			t.Errorf("%s: contract %d, want 1", kind, m.Contract)
		}
	}
}

func TestParseManifestTextAndDefaults(t *testing.T) {
	m, err := ParseManifest([]byte(`{"id":"dd","name":"DD","version":"1.0.0","kind":"downloader","run":["a"],
		"downloader":{"accepts":"any","parallel":0},
		"settings":[{"key":"k","type":"select","label":"Plain",
			"hint":{"en":"Hint","ru":"Подсказка"},
			"options":[{"value":1,"label":"One"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	f := m.Settings[0]
	if len(f.Label) != 1 || f.Label["en"] != "Plain" {
		t.Errorf("label = %v", f.Label)
	}
	if f.Hint.In("ru") != "Подсказка" || f.Hint.In("de") != "Hint" {
		t.Errorf("hint lookup: %q / %q", f.Hint.In("ru"), f.Hint.In("de"))
	}
	if (Text{"ru": "x"}).In("de") != "x" {
		t.Error("In should fall back to any language")
	}
	if f.Options[0].Label["en"] != "One" {
		t.Errorf("option label = %v", f.Options[0].Label)
	}
	if m.Downloader.Parallel != 1 {
		t.Errorf("parallel = %d, want 1", m.Downloader.Parallel)
	}
}

func TestParseManifestInvalid(t *testing.T) {
	withSettings := func(s string) string {
		return `{"id":"ok","name":"N","version":"1.0.0","kind":"tool","run":["a"],"settings":[` + s + `]}`
	}
	cases := []struct{ name, raw, field string }{
		{"id upper", `{"id":"Lucida","name":"N","version":"1.0.0","kind":"tool","run":["a"]}`, "id"},
		{"id dash", `{"id":"-x","name":"N","version":"1.0.0","kind":"tool","run":["a"]}`, "id"},
		{"id long", `{"id":"` + strings.Repeat("a", 41) + `","name":"N","version":"1.0.0","kind":"tool","run":["a"]}`, "id"},
		{"version", `{"id":"ok","name":"N","version":"1.2","kind":"tool","run":["a"]}`, "version"},
		{"kind", `{"id":"ok","name":"N","version":"1.0.0","kind":"widget","run":["a"]}`, "kind"},
		{"run", `{"id":"ok","name":"N","version":"1.0.0","kind":"tool","run":[]}`, "run"},
		{"no downloader", `{"id":"ok","name":"N","version":"1.0.0","kind":"downloader","run":["a"]}`, "downloader"},
		{"empty accepts", `{"id":"ok","name":"N","version":"1.0.0","kind":"downloader","run":["a"],"downloader":{"accepts":""}}`, "accepts"},
		{"field type", withSettings(`{"key":"k","type":"slider"}`), "type"},
		{"button action", withSettings(`{"key":"k","type":"button"}`), "action"},
		{"bad json", `{`, "manifest"},
	}
	for _, c := range cases {
		_, err := ParseManifest([]byte(c.raw))
		if err == nil {
			t.Errorf("%s: want error", c.name)
		} else if !strings.Contains(err.Error(), c.field) {
			t.Errorf("%s: error %q does not name %q", c.name, err, c.field)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("1.10.0", "1.9.9") <= 0 {
		t.Error("1.10.0 should be > 1.9.9")
	}
	if CompareVersions("1.9.9", "1.10.0") >= 0 {
		t.Error("1.9.9 should be < 1.10.0")
	}
	if CompareVersions("2.0.0", "2.0.0") != 0 {
		t.Error("2.0.0 should equal 2.0.0")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChecksumStable(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(a, "x.txt"), "one")
	writeFile(t, filepath.Join(a, "sub", "y.txt"), "two")
	writeFile(t, filepath.Join(b, "sub", "y.txt"), "two")
	writeFile(t, filepath.Join(b, "x.txt"), "one")
	sa, err := Checksum(a)
	if err != nil {
		t.Fatal(err)
	}
	sb, _ := Checksum(b)
	if sa != sb {
		t.Errorf("creation order changed the sum: %s vs %s", sa, sb)
	}
	writeFile(t, filepath.Join(b, "x.txt"), "onf")
	if sb2, _ := Checksum(b); sb2 == sa {
		t.Error("editing a byte kept the sum")
	}
}

func TestParseSums(t *testing.T) {
	m := ParseSums("a=1f, b=2e,junk,=x")
	if len(m) != 2 || m["a"] != "1f" || m["b"] != "2e" {
		t.Errorf("sums = %v", m)
	}
}
