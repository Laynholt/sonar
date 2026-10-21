package doubledouble

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestReadDevToolsPort(t *testing.T) {
	dir := t.TempDir()
	go func() {
		time.Sleep(50 * time.Millisecond)
		os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("51234\n/devtools/browser/abc\n"), 0o600)
	}()
	port, path, err := readDevToolsPort(context.Background(), dir)
	if err != nil || port != 51234 || path != "/devtools/browser/abc" {
		t.Fatalf("got %d %q %v", port, path, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := readDevToolsPort(ctx, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("want ctx.Err(), got %v", err)
	}
}

// fakeBrowser serves the DevTools endpoints; the page answers Runtime.evaluate from replies in order.
func fakeBrowser(t *testing.T, c *Client, replies []string) (endpoint string, browserMsgs *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var msgs []map[string]any
	var evals atomic.Int32
	up := websocket.Upgrader{}
	serve := func(onMsg func(m map[string]any) string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			ws, err := up.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer ws.Close()
			for {
				var m map[string]any
				if ws.ReadJSON(&m) != nil {
					return
				}
				ws.WriteMessage(websocket.TextMessage, []byte(`{"method":"Some.event"}`)) // events carry no id
				ws.WriteMessage(websocket.TextMessage, []byte(onMsg(m)))
			}
		}
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c.Base = srv.URL
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]map[string]string{
			{"type": "other", "url": "chrome://x", "webSocketDebuggerUrl": "ws://nowhere"},
			{"type": "page", "url": srv.URL + "/?url=x", "webSocketDebuggerUrl": "ws" + strings.TrimPrefix(srv.URL, "http") + "/devtools/page/1"},
		})
	})
	mux.HandleFunc("/devtools/browser/abc", serve(func(m map[string]any) string {
		mu.Lock()
		msgs = append(msgs, m)
		mu.Unlock()
		return `{"id":` + jsonNum(m["id"]) + `,"result":{}}`
	}))
	mux.HandleFunc("/devtools/page/1", serve(func(m map[string]any) string {
		i := int(evals.Add(1)) - 1
		r := replies[min(i, len(replies)-1)]
		return `{"id":` + jsonNum(m["id"]) + `,"result":{"result":` + r + `}}`
	}))
	return srv.URL, &msgs
}

func jsonNum(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestWatchReadsToken(t *testing.T) {
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = time.Second })
	c := New(filepath.Join(t.TempDir(), "token"))
	null := `{"type":"object","subtype":"null"}`
	endpoint, msgs := fakeBrowser(t, c, []string{null, null, `{"type":"string","value":"tok123"}`})

	tok, err := c.watch(context.Background(), endpoint, "/devtools/browser/abc", make(chan struct{}))
	if err != nil || tok != "tok123" {
		t.Fatalf("got %q %v", tok, err)
	}
	if len(*msgs) != 1 || (*msgs)[0]["method"] != "Browser.setDownloadBehavior" ||
		(*msgs)[0]["params"].(map[string]any)["behavior"] != "deny" {
		t.Fatalf("browser got %v", *msgs)
	}
}

func TestWatchWindowClosed(t *testing.T) {
	pollEvery = 5 * time.Millisecond
	t.Cleanup(func() { pollEvery = time.Second })
	c := New(filepath.Join(t.TempDir(), "token"))
	endpoint, _ := fakeBrowser(t, c, []string{`{"type":"object","subtype":"null"}`})
	exited := make(chan struct{})
	time.AfterFunc(20*time.Millisecond, func() { close(exited) })

	tok, err := c.watch(context.Background(), endpoint, "/devtools/browser/abc", exited)
	if !errors.Is(err, ErrCheckClosed) || tok != "" || c.Token() != "" {
		t.Fatalf("got %q %v, stored %q", tok, err, c.Token())
	}
}

func TestCheckRejectsSecondCheck(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "token"))
	c.checking.Store(true)
	err := c.Check(context.Background(), "no-such-browser", t.TempDir(), trackURL)
	if err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("got %v", err)
	}
}
