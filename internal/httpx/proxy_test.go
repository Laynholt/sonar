package httpx

import (
	"net/http"
	"testing"
)

func TestSetManualProxy(t *testing.T) {
	for _, c := range []struct {
		in, want string
		bad      bool
	}{
		{"", "", false},
		{"127.0.0.1:10808", "http://127.0.0.1:10808", false},
		{" socks5://127.0.0.1:10808 ", "socks5://127.0.0.1:10808", false},
		{"https://p.example:443", "https://p.example:443", false},
		{"ftp://x:1", "", true},
		{"http://", "", true},
	} {
		err := SetManualProxy(c.in)
		if (err != nil) != c.bad {
			t.Fatalf("%q: err=%v", c.in, err)
		}
		if !c.bad {
			if got := manualProxyString(); got != c.want {
				t.Fatalf("%q: got %q want %q", c.in, got, c.want)
			}
		}
	}
	SetManualProxy("")
}

func TestManualProxyWinsOverEnv(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://env:1")
	SetManualProxy("http://manual:2")
	defer SetManualProxy("")
	req, _ := http.NewRequest("GET", "https://open.spotify.com/", nil)
	u, _ := proxyFor(req)
	if u == nil || u.Host != "manual:2" {
		t.Fatalf("got %v", u)
	}
}

func TestParseScutilProxy(t *testing.T) {
	out := "<dictionary> {\n  HTTPEnable : 1\n  HTTPPort : 8080\n  HTTPProxy : 10.0.0.1\n  HTTPSEnable : 1\n  HTTPSPort : 10808\n  HTTPSProxy : 127.0.0.1\n  SOCKSEnable : 0\n}"
	if u := parseScutilProxy(out, "https"); u == nil || u.String() != "http://127.0.0.1:10808" {
		t.Fatalf("https: %v", u)
	}
	if u := parseScutilProxy(out, "http"); u == nil || u.String() != "http://10.0.0.1:8080" {
		t.Fatalf("http: %v", u)
	}
	socks := "<dictionary> {\n  SOCKSEnable : 1\n  SOCKSPort : 1080\n  SOCKSProxy : 127.0.0.1\n}"
	if u := parseScutilProxy(socks, "https"); u == nil || u.String() != "socks5://127.0.0.1:1080" {
		t.Fatalf("socks: %v", u)
	}
	if u := parseScutilProxy("<dictionary> {\n}", "https"); u != nil {
		t.Fatalf("none: %v", u)
	}
}

func TestParseGSettingsProxy(t *testing.T) {
	vals := map[string]string{
		"org.gnome.system.proxy mode":       "'manual'",
		"org.gnome.system.proxy.https host": "'127.0.0.1'", "org.gnome.system.proxy.https port": "10808",
		"org.gnome.system.proxy.socks host": "''", "org.gnome.system.proxy.socks port": "0",
	}
	get := func(schema, key string) string { return vals[schema+" "+key] }
	if u := parseGSettingsProxy(get, "https"); u == nil || u.String() != "http://127.0.0.1:10808" {
		t.Fatalf("got %v", u)
	}
	vals["org.gnome.system.proxy mode"] = "'none'"
	if u := parseGSettingsProxy(get, "https"); u != nil {
		t.Fatalf("mode none: %v", u)
	}
}

func TestPickWindowsProxyServer(t *testing.T) {
	// port SpotiDownloader behaviour: single host:port, per-scheme list, socks= entry
	if got := pickWindowsProxyServer("127.0.0.1:10808", "https"); got != "127.0.0.1:10808" {
		t.Fatal(got)
	}
	if got := pickWindowsProxyServer("http=a:1;https=b:2", "https"); got != "b:2" {
		t.Fatal(got)
	}
	if got := pickWindowsProxyServer("socks=c:3", "https"); got != "socks5://c:3" {
		t.Fatal(got)
	}
}

func TestLoopbackBypassesProxy(t *testing.T) {
	SetManualProxy("http://manual:2")
	defer SetManualProxy("")
	for _, raw := range []string{"http://127.0.0.1:1/", "http://localhost/", "http://[::1]/"} {
		req, _ := http.NewRequest("GET", raw, nil)
		if u, _ := proxyFor(req); u != nil {
			t.Fatalf("%s: got proxy %v", raw, u)
		}
	}
}
