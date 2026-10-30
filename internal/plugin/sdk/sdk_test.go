package sdk

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

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
