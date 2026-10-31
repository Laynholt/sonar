package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"sonar/internal/plugin"
)

func TestServeReturnsOnEOF(t *testing.T) {
	if err := ServeIO(&Server{}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestServeUnknownMethod(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"nope"}` + "\n")
	if err := ServeIO(&Server{}, in, &out); err != nil {
		t.Fatal(err)
	}
	var m plugin.Message
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatalf("%v: %q", err, out.String())
	}
	if m.ID == nil || *m.ID != 7 || m.Error == nil || m.Error.Code != -32601 {
		t.Fatalf("got %q", out.String())
	}
}

// lockedBuf is an io.Writer the test reads while ServeIO's goroutines write to it.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) lines() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Split(strings.TrimSpace(l.b.String()), "\n")
}

// A running download repeats its last progress (none yet: zeros) every heartbeat, so a silent stage does not
// trip sonar's watchdog; the beat stops before the answer, and other methods get none.
func TestHeartbeat(t *testing.T) {
	old := heartbeat
	heartbeat = 10 * time.Millisecond
	defer func() { heartbeat = old }()
	slow := func(_ context.Context, _ json.RawMessage, c *Call) (any, error) {
		time.Sleep(5 * heartbeat)
		c.Progress(5, 10, "x")
		time.Sleep(5 * heartbeat)
		return "ok", nil
	}
	quiet := func(context.Context, json.RawMessage, *Call) (any, error) {
		time.Sleep(10 * heartbeat)
		return "ok", nil
	}
	s := &Server{Methods: map[string]Handler{plugin.MDownload: slow, "other": quiet}}
	inR, inW := io.Pipe()
	out := &lockedBuf{}
	served := make(chan error)
	go func() { served <- ServeIO(s, inR, out) }()
	fmt.Fprintf(inW, `{"jsonrpc":"2.0","id":1,"method":%q}`+"\n"+`{"jsonrpc":"2.0","id":2,"method":"other"}`+"\n", plugin.MDownload)

	answered := func() int {
		n := 0
		for _, l := range out.lines() {
			if strings.Contains(l, `"result"`) {
				n++
			}
		}
		return n
	}
	for deadline := time.Now().Add(5 * time.Second); answered() < 2; time.Sleep(heartbeat) {
		if time.Now().After(deadline) {
			t.Fatalf("no answers: %q", out.lines())
		}
	}
	time.Sleep(5 * heartbeat) // a beat that outlived its handler would show up now
	inW.Close()
	if err := <-served; err != nil {
		t.Fatal(err)
	}

	var zeros, fives int
	var done bool
	for _, l := range out.lines() {
		var m plugin.Message
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("%v: %q", err, l)
		}
		if m.ID != nil {
			done = done || *m.ID == 1
			continue
		}
		var p plugin.ProgressParams
		json.Unmarshal(m.Params, &p)
		switch {
		case m.Method != plugin.MProgress || p.ID != 1:
			t.Errorf("unexpected note %q", l)
		case done:
			t.Errorf("progress after the answer: %q", l)
		case p.Progress == plugin.Progress{}:
			zeros++
		case p.Progress == plugin.Progress{Bytes: 5, Size: 10, Status: "x"}:
			fives++
		default:
			t.Errorf("unexpected progress %q", l)
		}
	}
	if zeros < 2 || fives < 3 { // fives: the handler's own note and at least two repeats
		t.Fatalf("zeros %d, fives %d: %q", zeros, fives, out.lines())
	}
}
