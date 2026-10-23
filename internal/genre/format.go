package genre

import (
	"strings"
	"unicode"
)

// Format upper-cases the letter at each word start (after a space or "&", so "r&b" gives "R&B"),
// leaves the rest as given and joins with ", "; single keeps only the first genre.
func Format(genres []string, single bool) string {
	if single && len(genres) > 1 {
		genres = genres[:1]
	}
	out := make([]string, len(genres))
	for i, g := range genres {
		up := true
		out[i] = strings.Map(func(r rune) rune {
			if up {
				r = unicode.ToUpper(r)
			}
			up = r == ' ' || r == '&'
			return r
		}, g)
	}
	return strings.Join(out, ", ")
}
