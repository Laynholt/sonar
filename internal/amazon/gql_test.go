package amazon

import (
	"os"
	"strings"
	"testing"
)

func TestDecodeGQL(t *testing.T) {
	read := func(name string) []byte {
		b, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	type albumResp struct{ Album *struct{ ID string } }

	var r albumResp
	if err := decodeGQL(read("gql_err_userpl.json"), &r); err == nil || !strings.Contains(err.Error(), "Invalid Authorization type") {
		t.Errorf("userpl: err = %v", err)
	}

	r = albumResp{}
	if err := decodeGQL(read("gql_album.json"), &r); err != nil || r.Album == nil || r.Album.ID != "B00A3SGWYC" {
		t.Errorf("album: %+v, %v", r.Album, err)
	}

	r = albumResp{}
	if err := decodeGQL(read("gql_err_notfound.json"), &r); err != nil || r.Album != nil {
		t.Errorf("notfound: %+v, %v", r.Album, err)
	}
}
