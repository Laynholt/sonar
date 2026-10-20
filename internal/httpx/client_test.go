package httpx

import (
	"net/http"
	"net/http/httptest"
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
