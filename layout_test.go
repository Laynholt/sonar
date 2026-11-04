package main

import (
	"archive/zip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sonar/internal/plugin"
)

func TestPluginDirs(t *testing.T) {
	off, cus := pluginDirs("E", "D")
	if off != filepath.Join("E", "plugins", "official") || cus != filepath.Join("E", "plugins", "custom") {
		t.Fatalf("with exe: %q %q", off, cus)
	}
	if off, cus = pluginDirs("", "D"); off != "" || cus != filepath.Join("D", "plugins", "custom") {
		t.Fatalf("without exe: %q %q", off, cus)
	}
}

func TestAppVersionMatchesWails(t *testing.T) {
	var w struct {
		Info struct {
			ProductVersion string `json:"productVersion"`
		} `json:"info"`
	}
	raw, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	ts, err := os.ReadFile(filepath.Join("frontend", "src", "lib", "version.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if w.Info.ProductVersion != appVersion || !strings.Contains(string(ts), `"`+appVersion+`"`) {
		t.Fatalf("appVersion %s, wails.json %s, version.ts %s", appVersion, w.Info.ProductVersion, ts)
	}
}

func TestMoveOldPlugins(t *testing.T) {
	exe, local := t.TempDir(), t.TempDir()
	official, custom := pluginDirs(exe, "")
	root := filepath.Join(exe, "plugins")
	writeFile(t, filepath.Join(root, "lucida", "plugin.json"), "old lucida")       // built-in id, official lacks it
	writeFile(t, filepath.Join(root, "spotify", "plugin.json"), "stale")           // built-in id, official has it
	writeFile(t, filepath.Join(official, "spotify", "plugin.json"), "new spotify") //
	writeFile(t, filepath.Join(root, "mine", "plugin.json"), "mine")               // not built-in
	writeFile(t, filepath.Join(root, "templates", "go", "plugin.json"), "tpl")     // no plugin.json of its own
	writeFile(t, filepath.Join(local, "theirs", "plugin.json"), "theirs")
	writeFile(t, filepath.Join(local, ".install-x", "plugin.json"), "temp")
	sums := map[string]string{"lucida": "1", "spotify": "2"}

	moveOldPlugins(official, custom, local, sums)

	for path, want := range map[string]string{
		filepath.Join(official, "lucida", "plugin.json"):      "old lucida",
		filepath.Join(official, "spotify", "plugin.json"):     "new spotify",
		filepath.Join(custom, "mine", "plugin.json"):          "mine",
		filepath.Join(custom, "theirs", "plugin.json"):        "theirs",
		filepath.Join(root, "templates", "go", "plugin.json"): "tpl",
	} {
		if got, err := os.ReadFile(path); err != nil || string(got) != want {
			t.Errorf("%s: %q, %v", path, got, err)
		}
	}
	for _, gone := range []string{"lucida", "spotify", "mine"} {
		if _, err := os.Lstat(filepath.Join(root, gone)); err == nil {
			t.Errorf("%s still in the old place", gone)
		}
	}
	if _, err := os.Lstat(filepath.Join(local, "theirs")); err == nil {
		t.Error("theirs still in LOCALAPPDATA")
	}
}

func TestSyncPlugins(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "plugin.json"), `{"id":"lucida","version":"1.0.0","kind":"tool","run":["a.exe"]}`)
	writeFile(t, filepath.Join(src, "a.exe"), "bin")
	sum, err := plugin.Checksum(src)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		zw := zip.NewWriter(w)
		for _, name := range []string{"plugin.json", "a.exe"} {
			f, _ := zw.Create("lucida/" + name)
			b, _ := os.ReadFile(filepath.Join(src, name))
			f.Write(b)
		}
		zw.Close()
	}))
	defer srv.Close()
	old := pluginsZipURL
	pluginsZipURL = srv.URL
	t.Cleanup(func() { pluginsZipURL = old })

	official := filepath.Join(t.TempDir(), "official")
	a := &App{dataDir: t.TempDir()}
	a.openPlugins(official, filepath.Join(t.TempDir(), "custom"), map[string]string{"lucida": sum})
	t.Cleanup(a.reg.Close)
	if err := a.builtinMissing(); err == nil {
		t.Fatal("official folder should be missing before sync")
	}
	ids, err := a.SyncPlugins()
	if err != nil || !slices.Equal(ids, []string{"lucida"}) {
		t.Fatalf("first sync: %v, %v", ids, err)
	}
	if e, ok := a.reg.Get("lucida"); !ok || !e.Builtin {
		t.Fatalf("lucida after sync: %+v", e)
	}
	if ids, err = a.SyncPlugins(); err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("second sync: %v, %v", ids, err)
	}
}
