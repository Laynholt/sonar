package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// putSettings writes a do-nothing plugin whose manifest has the given settings JSON array.
func putSettings(t *testing.T, root, id, version, settings string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	writeFile(t, filepath.Join(dir, "plugin.json"),
		`{"id":"`+id+`","name":"`+id+`","version":"`+version+`","kind":"tool","run":["a.exe"],"settings":`+settings+`}`)
	writeFile(t, filepath.Join(dir, "a.exe"), "bin-"+version)
	return dir
}

// putFake copies the built fake plugin to root/id under another id and version.
func putFake(t *testing.T, root, id, version string) string {
	t.Helper()
	dir := filepath.Join(root, id)
	entries, err := os.ReadDir(fakeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(fakeDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if e.Name() == "plugin.json" {
			b = []byte(`{"id":"` + id + `","name":"` + id + `","version":"` + version + `","kind":"tool","run":["fake"]}`)
		}
		writeFile(t, filepath.Join(dir, e.Name()), string(b))
	}
	return dir
}

// regEnv is a registry over temp folders.
type regEnv struct {
	data, builtin, user string
	sums                map[string]string
	l                   *logs
	r                   *Registry
}

func newEnv(t *testing.T) *regEnv {
	t.Helper()
	root := t.TempDir()
	e := &regEnv{data: filepath.Join(root, "data"), builtin: filepath.Join(root, "b"), user: filepath.Join(root, "u"),
		sums: map[string]string{}, l: &logs{}}
	return e
}

// bless records dir's checksum as the baked-in one for id.
func (e *regEnv) bless(t *testing.T, id, dir string) {
	t.Helper()
	sum, err := Checksum(dir)
	if err != nil {
		t.Fatal(err)
	}
	e.sums[id] = sum
}

func (e *regEnv) open(t *testing.T) *Registry {
	t.Helper()
	if e.r != nil {
		e.r.Close()
	}
	e.r = OpenRegistry(e.data, e.builtin, e.user, e.sums, func(string) InitParams { return InitParams{Lang: "en"} }, e.l.logf)
	t.Cleanup(e.r.Close)
	return e.r
}

func TestRegistryDefaults(t *testing.T) {
	e := newEnv(t)
	e.bless(t, "bi", putSettings(t, e.builtin, "bi", "1.0.0", `[{"key":"quality","type":"text","default":"flac"},{"key":"x","type":"note"}]`))
	putSettings(t, e.user, "third", "1.0.0", `[]`)
	r := e.open(t)

	bi, ok := r.Get("bi")
	if !ok || !bi.Enabled || !bi.Trusted || !reflect.DeepEqual(bi.Values, map[string]any{"quality": "flac"}) {
		t.Fatalf("built-in: %+v", bi)
	}
	th, ok := r.Get("third")
	if !ok || th.Enabled || th.Trusted || th.Sum == "" {
		t.Fatalf("third-party: %+v", th)
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatal("found a plugin that is not there")
	}
	if got := r.Enabled(KindTool); len(got) != 1 || got[0].M.ID != "bi" {
		t.Fatalf("Enabled: %+v", got)
	}
	if _, err := r.Host("third"); err == nil {
		t.Fatal("Host of a disabled plugin")
	}
	if err := r.SetEnabled("bi", false, false); err != nil {
		t.Fatal(err)
	}
	if bi, _ := r.Get("bi"); bi.Enabled {
		t.Fatal("built-in still on")
	}
}

func TestRegistryTrust(t *testing.T) {
	e := newEnv(t)
	dir := putSettings(t, e.user, "third", "1.0.0", `[]`)
	r := e.open(t)
	calls := 0
	r.OnChange = func() { calls++ }

	if err := r.SetEnabled("third", true, false); !errors.Is(err, ErrNeedsTrust) {
		t.Fatalf("got %v", err)
	}
	if calls != 0 || r.State().Enabled["third"] {
		t.Fatal("a refused enable changed something")
	}
	if err := r.SetEnabled("third", true, true); err != nil {
		t.Fatal(err)
	}
	g, _ := r.Get("third")
	if !g.Enabled || !g.Trusted || r.State().Trusted["third"] != g.Sum || calls != 1 {
		t.Fatalf("after confirm: %+v, %+v, %d calls", g, r.State(), calls)
	}

	writeFile(t, filepath.Join(dir, "a.exe"), "edited")
	r.Refresh()
	g, _ = r.Get("third")
	if g.Enabled || g.Trusted || !r.State().Enabled["third"] || calls != 2 {
		t.Fatalf("after edit: %+v, %+v, %d calls", g, r.State(), calls)
	}
	if _, err := r.Host("third"); err == nil {
		t.Fatal("Host of an edited plugin")
	}
	if err := r.SetEnabled("third", true, false); !errors.Is(err, ErrNeedsTrust) {
		t.Fatalf("got %v", err)
	}
	if err := r.SetEnabled("third", true, true); err != nil {
		t.Fatal(err)
	}
	if g, _ = r.Get("third"); !g.Enabled || !g.Trusted {
		t.Fatalf("after re-confirm: %+v", g)
	}
	if err := r.SetEnabled("nope", true, true); err == nil {
		t.Fatal("enabled a plugin that is not there")
	}
}

func TestRegistryOrder(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"a", "b", "c"} {
		putSettings(t, e.user, id, "1.0.0", `[]`)
	}
	ids := func(r *Registry) string {
		var s []string
		for _, en := range r.List() {
			s = append(s, en.M.ID)
		}
		return strings.Join(s, ",")
	}
	r := e.open(t)
	if got := ids(r); got != "a,b,c" {
		t.Fatalf("default order: %s", got)
	}
	if err := r.SetOrder([]string{"c", "gone", "a"}); err != nil {
		t.Fatal(err)
	}
	if got := ids(r); got != "c,a,b" {
		t.Fatalf("order: %s", got)
	}
	r = e.open(t)
	putSettings(t, e.user, "d", "1.0.0", `[]`)
	r.Refresh()
	if got := ids(r); got != "c,a,b,d" {
		t.Fatalf("after reopen and a new plugin: %s", got)
	}
}

func TestRegistrySettingsNotify(t *testing.T) {
	e := newEnv(t)
	e.bless(t, "fake1", putFake(t, e.builtin, "fake1", "1.0.0"))
	r := e.open(t)

	if err := r.SetSettings("nope", nil); err == nil {
		t.Fatal("saved settings of a plugin that is not there")
	}
	if err := r.SetSettings("fake1", map[string]any{"quality": "mp3"}); err != nil { // before it runs
		t.Fatal(err)
	}
	h, err := r.Host("fake1")
	if err != nil {
		t.Fatal(err)
	}
	initSettings := func(h *Host) map[string]any {
		t.Helper()
		var got map[string]any
		if err := h.Call(context.Background(), "initSettings", nil, &got, nil); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := initSettings(h); !reflect.DeepEqual(got, map[string]any{"quality": "mp3"}) {
		t.Fatalf("initialize got %v", got)
	}

	if err := r.SetSettings("fake1", map[string]any{"quality": "flac"}); err != nil { // while it runs
		t.Fatal(err)
	}
	e.l.wait(t, "got settings")

	if err := r.SetEnabled("fake1", false, false); err != nil { // a restart reads the saved values
		t.Fatal(err)
	}
	if err := r.SetEnabled("fake1", true, false); err != nil {
		t.Fatal(err)
	}
	h2, err := r.Host("fake1")
	if err != nil {
		t.Fatal(err)
	}
	if h2 == h {
		t.Fatal("same host after a restart")
	}
	if got := initSettings(h2); !reflect.DeepEqual(got, map[string]any{"quality": "flac"}) {
		t.Fatalf("after restart got %v", got)
	}
}

func TestRegistryStateFile(t *testing.T) {
	e := newEnv(t)
	putSettings(t, e.user, "third", "1.0.0", `[]`)
	r := e.open(t)
	if err := r.SetEnabled("third", true, true); err != nil {
		t.Fatal(err)
	}
	if err := r.SetSettings("third", map[string]any{"k": "v"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(e.data)
	if len(entries) != 1 || entries[0].Name() != "plugins.json" {
		t.Fatalf("data dir holds %v", entries)
	}
	raw, _ := os.ReadFile(filepath.Join(e.data, "plugins.json"))
	var st State
	if err := json.Unmarshal(raw, &st); err != nil || !st.Enabled["third"] || st.Settings["third"]["k"] != "v" || st.Trusted["third"] == "" {
		t.Fatalf("file: %s (%v)", raw, err)
	}
	if got := e.open(t).State(); !reflect.DeepEqual(got, st) {
		t.Fatalf("reopened: %+v, want %+v", got, st)
	}

	if err := r.SetState(State{Migrated: true}); err != nil { // nil maps come back empty
		t.Fatal(err)
	}
	if got := r.State(); !got.Migrated || got.Enabled == nil || got.Settings == nil || got.Trusted == nil || got.Order == nil {
		t.Fatalf("SetState: %+v", got)
	}

	if err := os.WriteFile(filepath.Join(e.data, "plugins.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.l = &logs{}
	got := e.open(t).State()
	if got.Migrated || len(got.Enabled) != 0 || got.Enabled == nil {
		t.Fatalf("corrupt file gave %+v", got)
	}
	e.l.wait(t, "plugins.json")
}

func TestRegistryDisableStopsHost(t *testing.T) {
	e := newEnv(t)
	e.bless(t, "fake1", putFake(t, e.builtin, "fake1", "1.0.0"))
	r := e.open(t)
	h, err := r.Host("fake1")
	if err != nil {
		t.Fatal(err)
	}
	echo(t, h)
	p := h.current()
	if err := r.SetEnabled("fake1", false, false); err != nil {
		t.Fatal(err)
	}
	if p.cmd.ProcessState == nil {
		t.Fatal("process still running")
	}
	if _, err := r.Host("fake1"); err == nil {
		t.Fatal("Host of a disabled plugin")
	}
}

func TestRegistryRefreshReplaced(t *testing.T) {
	e := newEnv(t)
	e.bless(t, "fake1", putFake(t, e.builtin, "fake1", "1.0.0"))
	r := e.open(t)
	h, err := r.Host("fake1")
	if err != nil {
		t.Fatal(err)
	}
	echo(t, h)
	p := h.current()

	newer := putFake(t, e.user, "fake1", "1.1.0")
	r.Refresh()
	if p.cmd.ProcessState == nil {
		t.Fatal("old process still running")
	}
	if err := r.SetEnabled("fake1", true, true); err != nil { // a user copy is third-party: confirm it
		t.Fatal(err)
	}
	h2, err := r.Host("fake1")
	if err != nil {
		t.Fatal(err)
	}
	if h2 == h || h2.f.Dir != newer {
		t.Fatalf("host: %v, dir %s", h2 == h, h2.f.Dir)
	}
	echo(t, h2)
}
