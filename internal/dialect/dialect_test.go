package dialect

import (
	"errors"
	"strings"
	"testing"

	"github.com/akfaiz/quill/internal/errs"
)

type grammars struct {
	name string
	g    Grammar
}

var all = []grammars{{"postgres", Postgres()}, {"mysql", MySQL()}, {"sqlite", SQLite()}}

// perDialect checks one method's result for each of the three grammars.
func perDialect[T comparable](t *testing.T, got func(Grammar) T, pg, my, lite T) {
	t.Helper()
	for i, want := range []T{pg, my, lite} {
		if g := got(all[i].g); g != want {
			t.Errorf("%s: got %#v, want %#v", all[i].name, g, want)
		}
	}
}

func TestQuote(t *testing.T) {
	tests := []struct{ in, pg, my, lite string }{
		{"id", `"id"`, "`id`", `"id"`},
		{`a"b`, `"a""b"`, "`a\"b`", `"a""b"`},
		{"a`b", "\"a`b\"", "`a``b`", "\"a`b\""},
		{"", `""`, "``", `""`},
		{"é 🙂", `"é 🙂"`, "`é 🙂`", `"é 🙂"`},
	}
	for _, tc := range tests {
		perDialect(t, func(g Grammar) string { return g.Quote(tc.in) }, tc.pg, tc.my, tc.lite)
	}
}

// FuzzQuoteCannotBeEscaped: the quoted identifier is the input wrapped in the quote
// character with every embedded quote doubled, so it can never close early.
func FuzzQuoteCannotBeEscaped(f *testing.F) {
	for _, s := range []string{"id", `a"b`, "a`b", `"`, "`", `""`, "x; drop table t", "", "\x00", "é"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		for _, tc := range []struct {
			g Grammar
			q string
		}{{Postgres(), `"`}, {SQLite(), `"`}, {MySQL(), "`"}} {
			out := tc.g.Quote(in)
			want := tc.q + strings.ReplaceAll(in, tc.q, tc.q+tc.q) + tc.q
			if out != want {
				t.Fatalf("Quote(%q) = %q, want %q", in, out, want)
			}
		}
	})
}

func TestPlaceholderAndUnboundedLimit(t *testing.T) {
	perDialect(t, func(g Grammar) string { return g.Placeholder(1) }, "$1", "?", "?")
	perDialect(t, func(g Grammar) string { return g.Placeholder(12) }, "$12", "?", "?")
	perDialect(t, func(g Grammar) string { return g.UnboundedLimit() }, "", "18446744073709551615", "-1")
}

func TestLikeOp(t *testing.T) {
	tests := []struct {
		cs, not        bool
		pg, my, sqlite string
	}{
		{false, false, "ilike", "like", "like"},
		{false, true, "not ilike", "not like", "not like"},
		{true, false, "like", "like binary", "glob"},
		{true, true, "not like", "not like binary", "not glob"},
	}
	for _, tc := range tests {
		perDialect(t, func(g Grammar) string { return g.LikeOp(tc.cs, tc.not) }, tc.pg, tc.my, tc.sqlite)
	}
}

func TestLikeValueOnlySQLiteGlobRewritesThePattern(t *testing.T) {
	perDialect(t, func(g Grammar) string { return g.LikeValue("a%b_c*d?", true) }, "a%b_c*d?", "a%b_c*d?", "a*b?c[*]d[?]")
	perDialect(t, func(g Grammar) string { return g.LikeValue("a%b_c*d?", false) }, "a%b_c*d?", "a%b_c*d?", "a%b_c*d?")
}

func TestLikeCastAndNullSafeOp(t *testing.T) {
	for _, op := range []string{"like", "ilike", "not like", "not ilike", "like binary"} {
		perDialect(t, func(g Grammar) string { return g.LikeCast(op) }, "::text", "", "")
	}
	for _, op := range []string{"=", ">", "is", "regexp"} {
		perDialect(t, func(g Grammar) string { return g.LikeCast(op) }, "", "", "")
	}
	perDialect(t, func(g Grammar) string { return g.NullSafeOp() }, "is not distinct from", "<=>", "is")
}

func TestDatePart(t *testing.T) {
	tests := []struct{ part, pg, my, lite string }{
		{"date", `"c"::date`, "date(`c`)", `strftime('%Y-%m-%d', "c")`},
		{"time", `"c"::time`, "time(`c`)", `strftime('%H:%M:%S', "c")`},
		{"day", `extract(day from "c")`, "day(`c`)", `strftime('%d', "c")`},
		{"month", `extract(month from "c")`, "month(`c`)", `strftime('%m', "c")`},
		{"year", `extract(year from "c")`, "year(`c`)", `strftime('%Y', "c")`},
	}
	for _, tc := range tests {
		for i, want := range []string{tc.pg, tc.my, tc.lite} {
			col := all[i].g.Quote("c")
			if got := all[i].g.DatePart(tc.part, col); got != want {
				t.Errorf("%s %s: got %s, want %s", all[i].name, tc.part, got, want)
			}
		}
	}
}

func TestDateValueWrap(t *testing.T) {
	for i, want := range [][2]string{{"", ""}, {"", ""}, {"cast(", " as text)"}} {
		pre, post := all[i].g.DateValueWrap()
		if pre != want[0] || post != want[1] {
			t.Errorf("%s: got (%q, %q), want %q", all[i].name, pre, post, want)
		}
	}
}

func TestJoinAndUnionRules(t *testing.T) {
	perDialect(t, func(g Grammar) bool { return g.SupportsStraightJoin() }, false, true, false)
	perDialect(t, func(g Grammar) bool { return g.SupportsLateralJoin() }, true, true, false)
	perDialect(t, func(g Grammar) string { return g.UnionOpen() }, "(", "(", "select * from (")
}

func TestLockSQL(t *testing.T) {
	tests := []struct {
		kind           LockKind
		raw            string
		pg, my, sqlite string
	}{
		{LockNone, "", "", "", ""},
		{LockUpdate, "", "for update", "for update", ""},
		{LockShared, "", "for share", "lock in share mode", ""},
		{LockRaw, "for no key update", "for no key update", "for no key update", ""}, // SQLite ignores every lock
		{LockRaw, "", "", "", ""},
	}
	for _, tc := range tests {
		perDialect(t, func(g Grammar) string { return g.LockSQL(tc.kind, tc.raw) }, tc.pg, tc.my, tc.sqlite)
	}
	// the raw text is only used for LockRaw
	if got := Postgres().LockSQL(LockUpdate, "ignored"); got != "for update" {
		t.Errorf("raw text leaked into LockUpdate: %q", got)
	}
}

func TestIndexHint(t *testing.T) {
	type result struct {
		hint string
		err  bool
	}
	tests := []struct {
		name, kind, index string
		pg, my, lite      result
	}{
		{"use", "use", "idx_a", result{}, result{"use index (idx_a)", false}, result{}},
		{"force", "force", "idx_a", result{}, result{"force index (idx_a)", false}, result{"indexed by idx_a", false}},
		{"ignore", "ignore", "idx_a", result{}, result{"ignore index (idx_a)", false}, result{}},
		{"mysql takes a list", "use", "a, b_2, $c", result{}, result{"use index (a, b_2, $c)", false}, result{}},
		{"sqlite takes one name", "force", "a, b", result{}, result{"force index (a, b)", false}, result{"", true}},
		{"injection attempt", "use", "a) ; drop table t; --", result{}, result{"", true}, result{}},
		{"empty name", "force", "", result{}, result{"", true}, result{"", true}},
		{"space inside a name", "force", "a b", result{}, result{"", true}, result{"", true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for i, want := range []result{tc.pg, tc.my, tc.lite} {
				hint, err := all[i].g.IndexHint(tc.kind, tc.index)
				if (err != nil) != want.err || hint != want.hint {
					t.Errorf("%s: got (%q, %v), want (%q, err=%v)", all[i].name, hint, err, want.hint, want.err)
				}
				if err != nil && !errors.Is(err, errs.ErrInvalidArgs) {
					t.Errorf("%s: error %v should be ErrInvalidArgs", all[i].name, err)
				}
			}
		})
	}
}

func TestRandom(t *testing.T) {
	seed := 42
	perDialect(t, func(g Grammar) string { return g.Random(nil) }, "RANDOM()", "RAND()", "RANDOM()")
	perDialect(t, func(g Grammar) string { return g.Random(&seed) }, "RANDOM()", "RAND(42)", "RANDOM()")
}

// MariaDB is MySQL's grammar except that it has no lateral joins.
func TestMariaDBInheritsMySQL(t *testing.T) {
	my, ma := MySQL(), MariaDB()
	seed := 42
	same := map[string][2]string{
		"Quote":          {my.Quote("a`b"), ma.Quote("a`b")},
		"Placeholder":    {my.Placeholder(3), ma.Placeholder(3)},
		"UnboundedLimit": {my.UnboundedLimit(), ma.UnboundedLimit()},
		"LikeOp":         {my.LikeOp(true, true), ma.LikeOp(true, true)},
		"LikeValue":      {my.LikeValue("a%", true), ma.LikeValue("a%", true)},
		"LikeCast":       {my.LikeCast("like"), ma.LikeCast("like")},
		"NullSafeOp":     {my.NullSafeOp(), ma.NullSafeOp()},
		"DatePart":       {my.DatePart("day", "c"), ma.DatePart("day", "c")},
		"UnionOpen":      {my.UnionOpen(), ma.UnionOpen()},
		"LockUpdate":     {my.LockSQL(LockUpdate, ""), ma.LockSQL(LockUpdate, "")},
		"LockShared":     {my.LockSQL(LockShared, ""), ma.LockSQL(LockShared, "")},
		"Random":         {my.Random(&seed), ma.Random(&seed)},
	}
	for name, pair := range same {
		if pair[0] != pair[1] {
			t.Errorf("%s: mysql %q, mariadb %q", name, pair[0], pair[1])
		}
	}
	pre1, post1 := my.DateValueWrap()
	pre2, post2 := ma.DateValueWrap()
	if pre1 != pre2 || post1 != post2 {
		t.Error("DateValueWrap differs")
	}
	h1, e1 := my.IndexHint("force", "idx")
	h2, e2 := ma.IndexHint("force", "idx")
	if h1 != h2 || (e1 == nil) != (e2 == nil) {
		t.Errorf("IndexHint differs: %q/%v vs %q/%v", h1, e1, h2, e2)
	}
	if my.SupportsStraightJoin() != ma.SupportsStraightJoin() || !ma.SupportsStraightJoin() {
		t.Error("straight_join should be supported on both")
	}
	if !my.SupportsLateralJoin() || ma.SupportsLateralJoin() {
		t.Errorf("lateral: mysql=%v (want true), mariadb=%v (want false)", my.SupportsLateralJoin(), ma.SupportsLateralJoin())
	}
}
