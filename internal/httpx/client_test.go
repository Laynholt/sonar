package httpx

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientSetsUserAgentOnlyWhenMissing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.UserAgent()))
	}))
	defer srv.Close()
	t.Setenv("NO_PROXY", "*")

	for _, c := range []struct{ set, want string }{{"", UserAgent}, {"custom/1", "custom/1"}} {
		req, _ := http.NewRequest("GET", srv.URL, nil)
		if c.set != "" {
			req.Header.Set("User-Agent", c.set)
		}
		resp, err := Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 512)
		n, _ := resp.Body.Read(buf)
		resp.Body.Close()
		if got := string(buf[:n]); got != c.want {
			t.Fatalf("got %q want %q", got, c.want)
		}
	}
}

func TestDeadManualProxyIsNotBypassed(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close() // nothing listens here any more
	t.Setenv("NO_PROXY", "")
	t.Cleanup(func() { SetManualProxy("") })

	for _, scheme := range []string{"http", "socks5"} {
		if err := SetManualProxy(scheme + "://" + addr); err != nil {
			t.Fatal(err)
		}
		_, err = Client().Get("http://spotify.test/")
		if err == nil || !strings.Contains(err.Error(), "proxy "+addr+" is unreachable") {
			t.Errorf("%s: err = %v", scheme, err)
		}
	}
}
