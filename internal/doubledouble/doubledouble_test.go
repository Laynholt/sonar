package doubledouble

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sonar/internal/source"
)

const trackURL = "https://music.amazon.com/tracks/B00A3SGXAA"

func newClient(t *testing.T, h http.Handler, token string) (*Client, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New(filepath.Join(t.TempDir(), "token"))
	c.Base = srv.URL
	c.PollEvery = time.Millisecond
	if token != "" {
		if err := c.SetToken(token); err != nil {
			t.Fatal(err)
		}
	}
	return c, filepath.Join(t.TempDir(), "track")
}

func TestDownloadFlow(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("url") != trackURL+"?musicTerritory=US" || q.Get("addmetadata") != "true" ||
			q.Get("donotshare") != "true" || q.Get("token") != "t1" {
			t.Errorf("bad query %v", q)
		}
		if _, ok := q["external"]; ok {
			t.Error("external must never be sent")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"id":"x","captchaToken":"t2"}`))
	})
	mux.HandleFunc("/dl/x", func(w http.ResponseWriter, r *http.Request) {
		if polls.Add(1) == 1 {
			w.Write([]byte(`{"status":"downloading","friendlyStatus":"Ripping","percent":42.5}`))
			return
		}
		w.Write([]byte(`{"status":"done","url":"./files/x.flac"}`))
	})
	mux.HandleFunc("/files/x.flac", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/flac")
		w.Write([]byte("fLaC data"))
	})
	c, base := newClient(t, mux, "t1")

	var seen []string
	path, err := c.Download(context.Background(), trackURL, base, func(s string) { seen = append(seen, s) })
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".flac" {
		t.Errorf("path %q", path)
	}
	if b, _ := os.ReadFile(path); string(b) != "fLaC data" {
		t.Errorf("content %q", b)
	}
	if got := c.Token(); got != "t2" {
		t.Errorf("token %q, want t2", got)
	}
	if len(seen) != 1 || seen[0] != "Ripping (43%)" {
		t.Errorf("statuses %q", seen)
	}
}

func TestDownloadNoTokenNeedsCheck(t *testing.T) {
	var hits atomic.Int32
	c, base := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }), "")
	_, err := c.Download(context.Background(), trackURL, base, nil)
	if !errors.Is(err, source.ErrNeedsCheck) {
		t.Fatalf("err %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("%d requests", hits.Load())
	}
}

func TestDownloadInvalidTokenNeedsCheck(t *testing.T) {
	c, base := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"error":"CAPTCHA bypass token is invalid."}`))
	}), "old")
	_, err := c.Download(context.Background(), trackURL, base, nil)
	if !errors.Is(err, source.ErrNeedsCheck) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(c.TokenPath); !os.IsNotExist(err) {
		t.Errorf("token file still there: %v", err)
	}
}

func TestDownloadOtherErrorIsPlain(t *testing.T) {
	c, base := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"success":false,"error":"Unsupported URL"}`))
	}), "t1")
	_, err := c.Download(context.Background(), trackURL, base, nil)
	if err == nil || errors.Is(err, source.ErrUnavailable) || !strings.Contains(err.Error(), "doubledouble: Unsupported URL") {
		t.Fatalf("err %v", err)
	}
	if c.Token() != "t1" {
		t.Error("token must survive a track-level error")
	}
}

func kickbackServer(t *testing.T, always bool) (http.Handler, *atomic.Int32) {
	var starts, polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) {
		starts.Add(1)
		w.Write([]byte(`{"success":true,"id":"x"}`))
	})
	mux.HandleFunc("/dl/x", func(w http.ResponseWriter, r *http.Request) {
		if always || polls.Add(1) == 1 {
			w.Write([]byte(`{"status":"error","kickback":true}`))
			return
		}
		w.Write([]byte(`{"status":"done","url":"` + "http://" + r.Host + `/f.mp3"}`))
	})
	mux.HandleFunc("/f.mp3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("id3"))
	})
	return mux, &starts
}

func TestDownloadKickback(t *testing.T) {
	h, starts := kickbackServer(t, false)
	c, base := newClient(t, h, "t1")
	path, err := c.Download(context.Background(), trackURL, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".mp3" || starts.Load() != 2 {
		t.Errorf("path %q, /dl called %d times", path, starts.Load())
	}

	h, starts = kickbackServer(t, true)
	c, base = newClient(t, h, "t1")
	_, err = c.Download(context.Background(), trackURL, base, nil)
	if err == nil || !strings.Contains(err.Error(), "kept restarting") {
		t.Fatalf("err %v", err)
	}
	if starts.Load() != 3 {
		t.Errorf("/dl called %d times, want 3", starts.Load())
	}
}

func TestDownloadPollError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"success":true,"id":"x"}`)) })
	mux.HandleFunc("/dl/x", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"error","message":"track not found"}`))
	})
	c, base := newClient(t, mux, "t1")
	_, err := c.Download(context.Background(), trackURL, base, nil)
	if err == nil || err.Error() != "doubledouble: track not found" {
		t.Fatalf("err %v", err)
	}
}

func TestDownloadUnavailable(t *testing.T) {
	c, base := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>Just a moment...</html>"))
	}), "t1")
	_, err := c.Download(context.Background(), trackURL, base, nil)
	if !errors.Is(err, source.ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
}

func TestDownloadCancelled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"success":true,"id":"x"}`)) })
	mux.HandleFunc("/dl/x", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"downloading"}`)) })
	c, base := newClient(t, mux, "t1")
	c.PollEvery = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	_, err := c.Download(ctx, trackURL, base, func(string) { once.Do(cancel) })
	if !errors.Is(err, context.Canceled) || errors.Is(err, source.ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
}

func TestSetTokenEmptyRemoves(t *testing.T) {
	c := New(filepath.Join(t.TempDir(), "token"))
	if err := c.SetToken(" abc \n"); err != nil || c.Token() != "abc" {
		t.Fatalf("token %q err %v", c.Token(), err)
	}
	if err := c.SetToken(""); err != nil || c.Token() != "" {
		t.Fatalf("token %q err %v", c.Token(), err)
	}
	if err := c.SetToken(""); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadPollUnknownJob(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"success":true,"id":"x"}`)) })
	mux.HandleFunc("/dl/x", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"success":false,"error":"job not found"}`))
	})
	c, base := newClient(t, mux, "t1")
	c.PollEvery = time.Hour // a poll loop would hang here
	_, err := c.Download(context.Background(), trackURL, base, nil)
	if err == nil || !strings.Contains(err.Error(), "job not found") {
		t.Fatalf("err %v", err)
	}
}

func TestDownloadStringPercent(t *testing.T) {
	var polls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"success":true,"id":"x"}`)) })
	mux.HandleFunc("/dl/x", func(w http.ResponseWriter, r *http.Request) {
		if polls.Add(1) == 1 {
			w.Write([]byte(`{"status":"uploading","friendlyStatus":"Packing","percent":"42%"}`))
			return
		}
		w.Write([]byte(`{"status":"done","url":"./f.mp3"}`))
	})
	mux.HandleFunc("/f.mp3", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("id3"))
	})
	c, base := newClient(t, mux, "t1")
	var seen []string
	if _, err := c.Download(context.Background(), trackURL, base, func(s string) { seen = append(seen, s) }); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "Packing" {
		t.Errorf("statuses %q", seen)
	}
}

// A server that takes each token once and hands out the next one, as the real site does.
func TestConcurrentStartsKeepNewestToken(t *testing.T) {
	var mu sync.Mutex
	valid, n := "t1", 1
	mux := http.NewServeMux()
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond) // widen the window in which both starts hold the same token
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Query().Get("token") != valid {
			w.Write([]byte(`{"success":false,"error":"CAPTCHA bypass token is invalid."}`))
			return
		}
		n++
		valid = "t" + strconv.Itoa(n)
		w.Write([]byte(`{"success":true,"id":"x","captchaToken":"` + valid + `"}`))
	})
	c, _ := newClient(t, mux, "t1")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = c.start(context.Background(), trackURL)
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("errs %v %v", errs[0], errs[1])
	}
	if got := c.Token(); got != valid {
		t.Errorf("token %q, want newest %q", got, valid)
	}
}

func TestClearTokenOnlyIfUnchanged(t *testing.T) {
	c, _ := newClient(t, http.NotFoundHandler(), "new")
	if err := c.clearToken("old"); err != nil || c.Token() != "new" {
		t.Fatalf("stale clear removed the token: %q, %v", c.Token(), err)
	}
	if err := c.clearToken("new"); err != nil || c.Token() != "" {
		t.Fatalf("clear kept the token: %q, %v", c.Token(), err)
	}
}

func TestWaitResolvesFileURL(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"./files/x.flac", "/files/x.flac"},
		{"/files/x.flac", "/files/x.flac"},
		{"https://cdn.example/x.flac", "https://cdn.example/x.flac"},
	} {
		c, _ := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"status":"done","url":"` + tc.in + `"}`))
		}), "")
		got, _, err := c.wait(context.Background(), "x", func(string) {})
		want := tc.want
		if strings.HasPrefix(want, "/") {
			want = c.Base + want
		}
		if err != nil || got != want {
			t.Errorf("%s: got %q, %v; want %q", tc.in, got, err, want)
		}
	}
}
