package db

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
)

// regexpAlias is the reference behaviour matchAlias must reproduce.
func regexpAlias(s string) (name, alias string, ok bool) {
	m := aliasRe.FindStringSubmatch(s)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

func assertSameAlias(t *testing.T, s string) {
	t.Helper()
	wn, wa, wok := regexpAlias(s)
	gn, ga, gok := matchAlias(s)
	assert.Equal(t, []any{wn, wa, wok}, []any{gn, ga, gok}, "input %q", s)
}

func TestMatchAliasEdgeCases(t *testing.T) {
	for _, s := range []string{
		"", "users", "users as u", "users AS u", "users As u", "users  as  u", "t.id as x",
		"a as", "a as ", "a as  ", "a as \n", "a as  \n", "as as as", " as x", "a  as b as c",
		"a\nas b", "a as b\nc", "a as\tb", "x as y z", "table as \t", "é as u", "ſ as u", "a aſ b", "a as ſ",
	} {
		assertSameAlias(t, s)
	}
}

func TestMatchAliasAgreesWithRegexp(t *testing.T) {
	alphabet := []string{"a", "s", "A", "S", " ", " ", "\t", "\n", "\f", "\r", "x", ".", "é", "ſ"}
	rng := rand.New(rand.NewSource(1))
	for range 200000 {
		var b []byte
		for l := rng.Intn(11); l > 0; l-- {
			b = append(b, alphabet[rng.Intn(len(alphabet))]...)
		}
		assertSameAlias(t, string(b))
	}
}

func FuzzMatchAlias(f *testing.F) {
	for _, s := range []string{"users as u", "a as  ", "a\nas b", "x AS y", "é as u"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) { assertSameAlias(t, s) })
}
