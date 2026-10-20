package main

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// confidentScore: at or above it the best match is shown as found, below it as "check it".
const confidentScore = 0.85

var (
	// "Song (feat. X)", "Song [Remastered]", "Song - Remastered 2011" -> "Song"
	titleSuffixRe = regexp.MustCompile(`\s*(\(.*?\)|\[.*?\]|\s-\s.*)$`)
	// A remaster is the same recording, unlike a remix or live take.
	remasterRe = regexp.MustCompile(`(?i)\s*(\([^)]*remaster[^)]*\)|\[[^\]]*remaster[^\]]*\]|\s-\s.*remaster.*$)`)
	noiseWords = map[string]bool{"feat": true, "ft": true, "featuring": true, "with": true, "the": true, "and": true, "remastered": true, "remaster": true}
)

// baseTitle drops trailing (feat. ...), [Remastered] and " - ..." parts for the search query.
func baseTitle(s string) string {
	for {
		t := titleSuffixRe.ReplaceAllString(s, "")
		if t == s || t == "" {
			return s
		}
		s = t
	}
}

func tokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if !noiseWords[w] {
			out[w] = true
		}
	}
	return out
}

// dice is 2|A∩B| / (|A|+|B|): 1 for the same words, 0 for none shared.
func dice(a, b map[string]bool) float64 {
	if len(a)+len(b) == 0 {
		return 0
	}
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return 2 * float64(n) / float64(len(a)+len(b))
}

// coverage is the share of a's words found in b.
func coverage(a, b map[string]bool) float64 {
	if len(a) == 0 {
		return 0
	}
	n := 0
	for w := range a {
		if b[w] {
			n++
		}
	}
	return float64(n) / float64(len(a))
}

func firstArtist(s string) string {
	return strings.TrimSpace(strings.Split(s, ",")[0])
}

func score(src, c Item) float64 {
	// Same base title with a different suffix ("Remastered", "feat.") is close, but below an exact title.
	noRemaster := func(s string) map[string]bool { return tokens(remasterRe.ReplaceAllString(s, "")) }
	title := max(dice(noRemaster(src.Title), noRemaster(c.Title)), 0.95*dice(tokens(baseTitle(src.Title)), tokens(baseTitle(c.Title))))
	artist := coverage(tokens(firstArtist(src.Artist)), tokens(c.Artist))
	s := 0.6*title + 0.4*artist
	if src.Explicit != c.Explicit {
		s -= 0.05 // prefer the same (clean/explicit) version
	}
	return s
}

// rank keeps candidates of the source's kind, best first, at most limit.
func rank(src Item, candidates []Item, limit int) []Item {
	var out []Item
	for _, c := range candidates {
		if c.Kind == src.Kind {
			c.Score = score(src, c)
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
