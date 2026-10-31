package main

import (
	"strings"
	"testing"
)

func TestCheckHintByLanguage(t *testing.T) {
	if h := checkHint("ru"); !strings.HasPrefix(h, "В этом окне снимите галку «Upload to»") {
		t.Errorf("ru: %q", h)
	}
	for _, lang := range []string{"en", "de", ""} {
		if h := checkHint(lang); !strings.HasPrefix(h, "In that window: untick “Upload to”") {
			t.Errorf("%q: %q", lang, h)
		}
	}
}
