package markdown

import "strings"

// Board order uses fractional ranks (spec/FORMAT.md §7): moving an issue
// rewrites only that issue's "rank", never its neighbours.

const rankDigits = "0123456789abcdefghijklmnopqrstuvwxyz"

// RankBetween returns a rank that sorts strictly after prev and before next.
// An empty prev means "first", an empty next means "last". Ranks that are not
// in order (edited by hand, say) fall back to "right after prev".
func RankBetween(prev, next string) string {
	prev, next = cleanRank(prev), cleanRank(next)
	if next != "" && prev >= next {
		next = ""
	}
	return rankMidpoint(prev, next)
}

func cleanRank(r string) string {
	r = strings.ToLower(strings.TrimSpace(r))
	for _, c := range r {
		if !strings.ContainsRune(rankDigits, c) {
			return ""
		}
	}
	return strings.TrimRight(r, "0")
}

// rankMidpoint follows the usual fractional-indexing construction: a and b are
// digit strings without trailing zeros, a < b, and b == "" stands for infinity.
func rankMidpoint(a, b string) string {
	if b != "" {
		n := 0
		for n < len(b) && rankDigit(a, n) == b[n] {
			n++
		}
		if n > 0 {
			rest := ""
			if n < len(a) {
				rest = a[n:]
			}
			return b[:n] + rankMidpoint(rest, b[n:])
		}
	}
	da := strings.IndexByte(rankDigits, rankDigit(a, 0))
	db := len(rankDigits)
	if b != "" {
		db = strings.IndexByte(rankDigits, b[0])
	}
	if db-da > 1 {
		return string(rankDigits[(da+db)/2])
	}
	if len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	return string(rankDigits[da]) + rankMidpoint(rest, "")
}

func rankDigit(s string, i int) byte {
	if i < len(s) {
		return s[i]
	}
	return '0'
}
