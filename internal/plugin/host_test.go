package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDir holds the built fake plugin and its plugin.json.
var fakeDir string

func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "sonar-plugin-test")
	if err != nil {
		panic(err)
	}
	fakeDir = filepath.Join(tmp, "fake")
	exe := "fake"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(fakeDir, exe), "./testdata/fake").CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build fake: %v\n%s", err, out))
	}
	manifest := `{"id":"fake","name":"Fake","version":"1.0.0","kind":"tool","run":["fake"]}`
	if err := os.WriteFile(filepath.Join(fakeDir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

// logs collects logf lines.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

// wait fails the test unless a line containing s shows up within 5 s.
func (l *logs) wait(t *testing.T, s string) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		l.mu.Lock()
		for _, line := range l.lines {
			if strings.Contains(line, s) {
				l.mu.Unlock()
				return
			}
		}
		l.mu.Unlock()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	t.Fatalf("no log line with %q in %q", s, l.lines)
}

func newFake(t *testing.T, dir string) (*Host, *logs) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	l := &logs{}
	init := func() InitParams {
		return InitParams{Contract: Contract, Lang: "en", Settings: map[string]any{"quality": "flac"}}
	}
	h := NewHost(Found{M: m, Dir: dir}, init, l.logf)
	t.Cleanup(h.Close)
	return h, l
}

func echo(t *testing.T, h *Host) {
	t.Helper()
	var got map[string]any
	if err := h.Call(context.Background(), "echo", map[string]any{"a": "b"}, &got, nil); err != nil {
		t.Fatal(err)
	}
	if got["a"] != "b" {
		t.Fatalf("echo: got %v", got)
	}
}

// current returns the running process, or nil.
func (h *Host) current() *proc {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.p
}

func TestHostEcho(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	if h.current() != nil {
		t.Fatal("started before the first Call")
	}
	echo(t, h)
	if h.current() == nil {
		t.Fatal("not running after a Call")
	}
}

// A Python plugin must not write __pycache__ into its folder: that changes its checksum and drops its trust.
func TestHostNoBytecode(t *testing.T) {
	t.Setenv("PYTHONDONTWRITEBYTECODE", "") // sonar's own environment must not decide it
	h, _ := newFake(t, fakeDir)
	var got string
	if err := h.Call(context.Background(), "getenv", "PYTHONDONTWRITEBYTECODE", &got, nil); err != nil || got != "1" {
		t.Fatalf("PYTHONDONTWRITEBYTECODE = %q, %v", got, err)
	}
}

func TestHostInitParams(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	var got map[string]any
	if err := h.Call(context.Background(), "initSettings", nil, &got, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, map[string]any{"quality": "flac"}) {
		t.Fatalf("got %v", got)
	}
}

func TestHostProgress(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	var notes []Progress
	var got string
	err := h.Call(context.Background(), "progress", nil, &got, func(p Progress) { notes = append(notes, p) })
	if err != nil {
		t.Fatal(err)
	}
	if got != "done" || len(notes) != 3 {
		t.Fatalf("got %q after %v", got, notes)
	}
	for i, p := range notes {
		if p.Bytes != int64(i+1) || p.Size != 3 || p.Status != fmt.Sprint("step ", i+1) {
			t.Fatalf("note %d: %+v", i, p)
		}
	}
}

func TestHostCancel(t *testing.T) {
	h, l := newFake(t, fakeDir)
	echo(t, h)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)
	start := time.Now()
	err := h.Call(ctx, "block", nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond+time.Second {
		t.Fatalf("returned after %v", d)
	}
	l.wait(t, "block cancelled")
	echo(t, h)
}

func TestHostCrashRestart(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	h.backoff = 2 * time.Second
	err := h.Call(context.Background(), "crash", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("crash: got %v", err)
	}
	if err := h.Call(context.Background(), "echo", nil, nil, nil); err == nil || !strings.Contains(err.Error(), "restarting") {
		t.Fatalf("within backoff: got %v", err)
	}
	if !strings.Contains(h.LastCrash(), "boom") {
		t.Fatalf("LastCrash: %q", h.LastCrash())
	}
	time.Sleep(2200 * time.Millisecond)
	echo(t, h)
	if h.LastCrash() != "" {
		t.Fatalf("LastCrash after a clean start: %q", h.LastCrash())
	}
}

func TestHostHungDownload(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	h.stall = 300 * time.Millisecond
	echo(t, h)
	p := h.current()
	notes := 0
	err := h.Call(context.Background(), "hang", nil, nil, func(Progress) { notes++ })
	if err == nil || !strings.Contains(err.Error(), "no progress for 0.3 s") {
		t.Fatalf("got %v", err)
	}
	if notes != 1 {
		t.Fatalf("notes: %d", notes)
	}
	if p.cmd.ProcessState == nil {
		t.Fatal("process still running")
	}
	if !strings.Contains(h.LastCrash(), "no progress") {
		t.Fatalf("LastCrash: %q", h.LastCrash())
	}
}

func TestHostInitTimeout(t *testing.T) {
	t.Setenv("FAKE_INIT_SLEEP", "11s")
	h, _ := newFake(t, fakeDir)
	h.initTimeout = 300 * time.Millisecond
	err := h.Call(context.Background(), "echo", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("got %v", err)
	}
}

func TestHostErrorCodes(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	err := h.Call(context.Background(), "unavailable", nil, nil, nil)
	var e *Error
	if !errors.As(err, &e) || e.Code != CodeUnavailable || e.Message != "down" {
		t.Fatalf("got %#v", err)
	}
}

func TestHostSkipsJunkLines(t *testing.T) {
	h, l := newFake(t, fakeDir)
	var got string
	if err := h.Call(context.Background(), "junk", nil, &got, nil); err != nil || got != "ok" {
		t.Fatalf("got %q, %v", got, err)
	}
	l.wait(t, "not json")
}

func TestHostStatus(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	got := make(chan PluginStatus, 1)
	h.OnStatus = func(st PluginStatus) { got <- st }
	if err := h.Call(context.Background(), "status", nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case st := <-got:
		if st.Available || st.NeedsAction == nil || st.NeedsAction.Action != "check" {
			t.Fatalf("got %+v", st)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no status")
	}
}

func TestHostStderrLogged(t *testing.T) {
	h, l := newFake(t, fakeDir)
	echo(t, h)
	l.wait(t, "fake: fake started")
}

func TestHostCloseKills(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	echo(t, h)
	p := h.current()
	h.Close()
	if p.cmd.ProcessState == nil {
		t.Fatal("process still running")
	}
	if err := h.Call(context.Background(), "echo", nil, nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
	if err := h.Notify(MSettingsChanged, InitParams{}); err != nil {
		t.Fatalf("Notify after Close: %v", err)
	}
	h.Close()
}

func TestHostStartsFromUnicodeDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Михаил Иванов", "plugins", "fake")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(fakeDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(fakeDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h, _ := newFake(t, dir)
	echo(t, h)
}

func TestNotifyIdleIsNoop(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	if err := h.Notify(MSettingsChanged, InitParams{}); err != nil || h.current() != nil {
		t.Fatalf("got %v, running %v", err, h.current() != nil)
	}
	echo(t, h)
	if err := h.Notify(MSettingsChanged, json.RawMessage(`{}`)); err != nil {
		t.Fatal(err)
	}
}

// A watchdog that fires while the reader holds a note for the call must not wedge the reader:
// the call keeps draining until the process is gone, so Call and Close both return.
func TestHostStallWithNotesQueued(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	h.stall = 50 * time.Millisecond
	h.backoff = 0
	for i := 0; i < 10; i++ { // the stall and the next note race; each round picks one at random
		done := make(chan error, 1)
		go func() {
			first := true
			done <- h.Call(context.Background(), "progress", nil, nil, func(Progress) {
				if first { // outlast the watchdog while the reader waits with the next note
					first = false
					time.Sleep(150 * time.Millisecond)
				}
			})
		}()
		select {
		case err := <-done:
			if err != nil && !strings.Contains(err.Error(), "no progress") {
				t.Fatalf("round %d: %v", i, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: Call hung", i)
		}
	}
}

func TestHostCloseKillsGrandchild(t *testing.T) {
	h, _ := newFake(t, fakeDir)
	var pid int
	if err := h.Call(context.Background(), "spawn", nil, &pid, nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { // the tree kill is Windows-only; elsewhere tidy up here
		if p, err := os.FindProcess(pid); err == nil {
			p.Kill()
		}
	})
	start := time.Now()
	h.Close()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Close took %v", d)
	}
	if runtime.GOOS != "windows" {
		return
	}
	for end := time.Now().Add(2 * time.Second); alive(pid); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("grandchild %d still runs", pid)
		}
	}
}

func TestHostNotifyWaitsForInitialize(t *testing.T) {
	t.Setenv("FAKE_INIT_SLEEP", "300ms")
	h, l := newFake(t, fakeDir)
	done := make(chan struct{})
	go func() {
		defer close(done)
		echo(t, h)
	}()
	for h.current() == nil {
		time.Sleep(time.Millisecond)
	}
	if err := h.Notify(MSettingsChanged, InitParams{}); err != nil {
		t.Fatal(err)
	}
	<-done
	echo(t, h) // the plugin reads lines in order, so a note sent earlier is handled by now
	l.mu.Lock()
	early := strings.Contains(strings.Join(l.lines, "\n"), "got settings")
	l.mu.Unlock()
	if early {
		t.Fatal("a note reached the plugin before initialize")
	}
	if err := h.Notify(MSettingsChanged, InitParams{}); err != nil {
		t.Fatal(err)
	}
	l.wait(t, "got settings")
}

// count returns how many logged lines contain s.
func (l *logs) count(s string) (n int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, line := range l.lines {
		if strings.Contains(line, s) {
			n++
		}
	}
	return n
}
