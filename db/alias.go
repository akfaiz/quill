package db

import "strings"

// matchAlias splits "name as alias". It is the allocation-free equivalent of
// aliasRe.FindStringSubmatch for ASCII input: whitespace is RE2's \s
// ([\t\n\f\r ]), "as" is case-insensitive, "." never matches a newline, and the
// first (shortest) name wins. Anything non-ASCII goes through the regexp itself,
// because (?i) folds some non-ASCII runes onto "s" (for example U+017F).
func matchAlias(s string) (name, alias string, ok bool) {
	if !strings.ContainsAny(s, " \t\n\f\r") {
		return "", "", false // an alias needs whitespace around "as"
	}
	for i := range len(s) {
		if s[i] >= 0x80 {
			m := aliasRe.FindStringSubmatch(s)
			if m == nil {
				return "", "", false
			}
			return m[1], m[2], true
		}
	}
	for i := 1; i < len(s); i++ {
		if s[i-1] == '\n' {
			return "", "", false // the name cannot contain a newline
		}
		j := skipSpace(s, i)
		if j == i || j+2 > len(s) || !isAS(s[j], s[j+1]) {
			continue
		}
		k := j + 2
		m := skipSpace(s, k)
		if m == k {
			continue
		}
		switch {
		case m < len(s):
			if !strings.Contains(s[m:], "\n") {
				return s[:i], s[m:], true
			}
		case m-1 > k && s[m-1] != '\n':
			// only spaces follow: \s+ gives one back so (.+) has something to match
			return s[:i], s[m-1:], true
		}
	}
	return "", "", false
}

func skipSpace(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\f' || s[i] == '\r') {
		i++
	}
	return i
}

func isAS(a, b byte) bool { return (a == 'a' || a == 'A') && (b == 's' || b == 'S') }
