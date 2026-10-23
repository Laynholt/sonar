package genre

import "testing"

func TestFormat(t *testing.T) {
	cases := []struct {
		in     []string
		single bool
		want   string
	}{
		{[]string{"russian hip hop", "Rap"}, false, "Russian Hip Hop, Rap"},
		{[]string{"russian hip hop", "Rap"}, true, "Russian Hip Hop"},
		{nil, false, ""},
		{[]string{"r&b"}, false, "R&B"},
		{[]string{"k-pop"}, false, "K-pop"},
	}
	for _, c := range cases {
		if got := Format(c.in, c.single); got != c.want {
			t.Errorf("Format(%q,%v)=%q want %q", c.in, c.single, got, c.want)
		}
	}
}
