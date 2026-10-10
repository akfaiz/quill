package db_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

// FuzzIdentifierQuoting checks that a plain column name can never escape its
// quotes: the output must be exactly the name wrapped with its quote character
// doubled inside.
func FuzzIdentifierQuoting(f *testing.F) {
	for _, s := range []string{"id", `a"b`, "a`b", "x; drop table t", "*", "", "é", `"`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, col string) {
		// names with structure (qualifiers, aliases, JSON paths) are covered by golden tests
		if col == "*" || col == "" || strings.ContainsAny(col, ".\t\n\v\f\r ") || strings.Contains(col, "->") {
			t.Skip()
		}
		for _, tc := range []struct {
			d db.Dialect
			q string
		}{{db.Postgres, `"`}, {db.SQLite, `"`}, {db.MySQL, "`"}} {
			sql, _, err := db.Use(tc.d).Table("t").Select(col).ToSQL()
			require.NoError(t, err)
			want := "select " + tc.q + strings.ReplaceAll(col, tc.q, tc.q+tc.q) + tc.q + " from " + tc.q + "t" + tc.q
			assert.Equal(t, want, sql)
		}
	})
}

// FuzzRawPlaceholders checks the "?" translator never panics, never invents
// arguments, and numbers Postgres placeholders sequentially.
func FuzzRawPlaceholders(f *testing.F) {
	for _, s := range []string{"a = ?", "a ?? 'k'", "'?' and ?", `"?" = ?`, "`?` ?", "?", "", "'unterminated ?", "??", "???"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		sql, args, err := db.Use(db.Postgres).Table("t").WhereRaw(raw, 1, 2).Where("z", 3).ToSQL()
		require.NoError(t, err)
		if len(args) < 1 || len(args) > 3 {
			t.Fatalf("got %d args for %q", len(args), raw)
		}
		if last := args[len(args)-1]; last != 3 {
			t.Fatalf("the trailing bound arg must stay last, got %v in %q", args, sql)
		}
		if !strings.HasSuffix(sql, `"z" = $`+string(rune('0'+len(args)))) {
			t.Fatalf("placeholder numbering broke for %q: %s (args %v)", raw, sql, args)
		}
	})
}
