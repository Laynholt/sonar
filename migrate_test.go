package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

// oldApp is an App on dir with an empty plugin registry, before migrate has run.
func oldApp(t *testing.T, dir string) *App {
	t.Helper()
	a := &App{dataDir: dir}
	a.openPlugins(t.TempDir(), filepath.Join(t.TempDir(), "user"), map[string]string{})
	t.Cleanup(a.reg.Close)
	return a
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

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

const oldConfig = `{"rateGuard":false,"rateGuardLimit":80,"rateGuardWait":60,"embedGenre":false,"useSingleGenre":true,"downloadSource":"lucida"}`

func TestMigrateSettings(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), oldConfig)
	a := oldApp(t, dir)
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	st := a.reg.State()
	for _, id := range []string{"lucida", "doubledouble"} {
		want := map[string]any{guardLimitKey: 0, guardWaitKey: 60}
		if got := st.Settings[id]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s settings = %v, want %v", id, got, want)
		}
	}
	if on, set := st.Enabled["chosic"]; !set || on {
		t.Errorf("chosic enabled = %v (set %v), want off", on, set)
	}
	if got := st.Settings["chosic"]["singleGenre"]; got != true {
		t.Errorf("singleGenre = %v", got)
	}
	if !st.Migrated {
		t.Error("Migrated is false")
	}
	if s, _ := a.LoadSettings(); s["downloadSource"] != "lucida" || s["rateGuardLimit"] == nil {
		t.Errorf("config.json lost keys: %v", s)
	}
}

func TestMigrateLimitWithoutSwitch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), `{"rateGuard":true,"rateGuardLimit":80,"embedGenre":true}`)
	a := oldApp(t, dir)
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	st := a.reg.State()
	for _, id := range []string{"lucida", "doubledouble"} {
		if want := (map[string]any{guardLimitKey: 80}); !reflect.DeepEqual(st.Settings[id], want) {
			t.Errorf("%s settings = %v, want %v", id, st.Settings[id], want)
		}
	}
	if _, set := st.Enabled["chosic"]; set {
		t.Error("embedGenre true switched chosic")
	}
}

func TestMigrateKeepsWhatIsThere(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), `{"rateGuardLimit":80,"rateGuardWait":15}`)
	a := oldApp(t, dir)
	st := a.reg.State()
	st.Settings["lucida"] = map[string]any{"other": "x", guardWaitKey: 120}
	if err := a.reg.SetState(st); err != nil {
		t.Fatal(err)
	}
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	st = a.reg.State()
	if want := (map[string]any{"other": "x", guardWaitKey: 120, guardLimitKey: 80}); !reflect.DeepEqual(st.Settings["lucida"], want) {
		t.Errorf("lucida = %v, want %v", st.Settings["lucida"], want)
	}
	if _, set := st.Enabled["chosic"]; set || st.Settings["chosic"] != nil {
		t.Errorf("keys absent from the old config produced chosic state: %v %v", st.Enabled, st.Settings["chosic"])
	}
}

func TestMigrateFreshInstall(t *testing.T) {
	a := oldApp(t, t.TempDir())
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	if !a.reg.State().Migrated {
		t.Error("a fresh install is not marked migrated")
	}
}

func TestMigrateFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "genres.json"), `{"a":1}`)
	writeFile(t, filepath.Join(dir, "doubledouble-token"), "tok")
	writeFile(t, filepath.Join(dir, "doubledouble-browser", "Default", "Cookies"), "c")
	a := oldApp(t, dir)
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	for moved, want := range map[string]string{
		filepath.Join("plugins-data", "chosic", "genres.json"):                         `{"a":1}`,
		filepath.Join("plugins-data", "doubledouble", "token"):                         "tok",
		filepath.Join("plugins-data", "doubledouble", "profile", "Default", "Cookies"): "c",
	} {
		if got, err := os.ReadFile(filepath.Join(dir, moved)); err != nil || string(got) != want {
			t.Errorf("%s = %q, %v; want %q", moved, got, err, want)
		}
	}
	for _, old := range []string{"genres.json", "doubledouble-token", "doubledouble-browser"} {
		if exists(filepath.Join(dir, old)) {
			t.Errorf("%s was not removed", old)
		}
	}
}

func TestMigrateFailedMoveRetries(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "genres.json"), `{"a":1}`)
	writeFile(t, filepath.Join(dir, "doubledouble-token"), "tok")
	writeFile(t, filepath.Join(dir, "doubledouble-browser", "x"), "c")
	writeFile(t, filepath.Join(dir, "config.json"), oldConfig)
	// A file where chosic's folder should go: its copy cannot be written.
	writeFile(t, filepath.Join(dir, "plugins-data", "chosic"), "in the way")
	a := oldApp(t, dir)
	if err := a.migrate(); err == nil {
		t.Fatal("migrate hid a failed copy")
	}
	st := a.reg.State()
	if st.Migrated {
		t.Error("Migrated after a failed copy")
	}
	if !exists(filepath.Join(dir, "genres.json")) {
		t.Error("the old file went with the failed copy")
	}
	if st.Settings["lucida"][guardLimitKey] != 0 {
		t.Error("the settings wait for the files")
	}
	// The other moves still happened.
	if !exists(filepath.Join(dir, "plugins-data", "doubledouble", "token")) || exists(filepath.Join(dir, "doubledouble-token")) {
		t.Error("the token did not move")
	}

	os.Remove(filepath.Join(dir, "plugins-data", "chosic"))
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	if !a.reg.State().Migrated || exists(filepath.Join(dir, "genres.json")) || !exists(filepath.Join(dir, "plugins-data", "chosic", "genres.json")) {
		t.Error("the retry did not finish the move")
	}
}

func TestMigrateKeepsExistingDestination(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "genres.json"), "old")
	writeFile(t, filepath.Join(dir, "plugins-data", "chosic", "genres.json"), "new")
	writeFile(t, filepath.Join(dir, "doubledouble-browser", "x"), "old")
	writeFile(t, filepath.Join(dir, "plugins-data", "doubledouble", "profile", "y"), "new")
	a := oldApp(t, dir)
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "plugins-data", "chosic", "genres.json")); string(got) != "new" {
		t.Errorf("genres.json overwritten: %q", got)
	}
	profile := filepath.Join(dir, "plugins-data", "doubledouble", "profile")
	if exists(filepath.Join(profile, "x")) || !exists(filepath.Join(profile, "y")) {
		t.Error("profile overwritten or merged")
	}
	if !a.reg.State().Migrated {
		t.Error("an existing destination is not a failure")
	}
}

func TestMigrateOnce(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), oldConfig)
	writeFile(t, filepath.Join(dir, "genres.json"), "g")
	a := oldApp(t, dir)
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	st := a.reg.State()
	st.Settings["lucida"][guardLimitKey] = 7 // the user's change after the first run
	if err := a.reg.SetState(st); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "genres.json"), "again")
	if err := a.migrate(); err != nil {
		t.Fatal(err)
	}
	if got := a.reg.State().Settings["lucida"][guardLimitKey]; got != 7 {
		t.Errorf("second run reset the limit to %v", got)
	}
	if !exists(filepath.Join(dir, "genres.json")) {
		t.Error("second run moved a file")
	}
}

// An old queue.json with amazon_id items and a guard pause still resumes once the settings are migrated.
func TestMigrationKeepsQueueAndPauses(t *testing.T) {
	dir := t.TempDir()
	until := time.Now().Add(time.Hour).UnixMilli()
	out, _ := json.Marshal(t.TempDir())
	writeFile(t, filepath.Join(dir, "config.json"), oldConfig)
	writeFile(t, filepath.Join(dir, "queue.json"), `{"version":1,"items":[{"item":{"id":"1","track":{"id":"1","service":"spotify","title":"T"},
		"amazon_id":"B00A3SGXA1","status":"queued"},"collection":{"kind":"track","id":"1","name":"T"},
		"options":{"dir":`+string(out)+`,"format":"original","source":"lucida","filename_template":"{title}"}}],
		"pauses":{"lucida":`+strconv.FormatInt(until, 10)+`}}`)
	a := newPluginApp(t, dir, nil, stdPlugs("")...)
	items := a.GetDownloadState().Items
	if len(items) != 1 || items[0].IDs["amazon"] != "B00A3SGXA1" {
		t.Fatalf("restored items = %+v, want one with the amazon id", items)
	}
	if got := site(a, "lucida"); got.PausedUntil != until {
		t.Fatalf("lucida PausedUntil = %d, want %d", got.PausedUntil, until)
	}
	if st := a.reg.State(); !st.Migrated || st.Settings["lucida"][guardLimitKey] != 0 {
		t.Errorf("state after startup = %+v", st)
	}
}
