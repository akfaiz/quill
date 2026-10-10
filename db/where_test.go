package db_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"

	"github.com/akfaiz/quill/db"
)

func newT(d db.Dialect) *db.Builder { return db.Use(d).Table("t") }

func TestWhereFamilyGolden(t *testing.T) {
	tests := []golden{
		{
			name: "where not and or not",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereNot("a", 1).OrWhereNot("b", ">", 2).WhereNot("c", nil)
			},
			pg:   `select * from "t" where not "a" = $1 or not "b" > $2 and not "c" is null`,
			my:   "select * from `t` where not `a` = ? or not `b` > ? and not `c` is null",
			lite: `select * from "t" where not "a" = ? or not "b" > ? and not "c" is null`,
			args: []any{1, 2},
		},
		{
			name: "where column",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereColumn("a.x", "b.y").OrWhereColumn("first", "<>", "last")
			},
			pg:   `select * from "t" where "a"."x" = "b"."y" or "first" <> "last"`,
			my:   "select * from `t` where `a`.`x` = `b`.`y` or `first` <> `last`",
			lite: `select * from "t" where "a"."x" = "b"."y" or "first" <> "last"`,
		},
		{
			name: "in notin slice variants and empty",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).
					WhereIn("id", []int{1, 2, 3}).
					OrWhereIn("s", []string{"a"}).
					WhereNotIn("n", []any{4, 5}).
					WhereIn("none", []int{}).
					WhereNotIn("none", []string{})
			},
			pg:   `select * from "t" where "id" in ($1, $2, $3) or "s" in ($4) and "n" not in ($5, $6) and 0 = 1 and 1 = 1`,
			my:   "select * from `t` where `id` in (?, ?, ?) or `s` in (?) and `n` not in (?, ?) and 0 = 1 and 1 = 1",
			lite: `select * from "t" where "id" in (?, ?, ?) or "s" in (?) and "n" not in (?, ?) and 0 = 1 and 1 = 1`,
			args: []any{1, 2, 3, "a", 4, 5},
		},
		{
			name: "in subquery shares arg numbering",
			build: func(d db.Dialect) *db.Builder {
				sub := db.Use(d).Table("bans").Select("user_id").Where("reason", "spam").Where("level", ">", 2)
				return newT(d).Where("a", 1).WhereIn("id", sub).Where("z", 9)
			},
			pg:   `select * from "t" where "a" = $1 and "id" in (select "user_id" from "bans" where "reason" = $2 and "level" > $3) and "z" = $4`,
			my:   "select * from `t` where `a` = ? and `id` in (select `user_id` from `bans` where `reason` = ? and `level` > ?) and `z` = ?",
			lite: `select * from "t" where "a" = ? and "id" in (select "user_id" from "bans" where "reason" = ? and "level" > ?) and "z" = ?`,
			args: []any{1, "spam", 2, 9},
		},
		{
			name: "scalar subquery value",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).Where("score", ">", db.Use(d).Table("avgs").Select("v").Where("k", 7))
			},
			pg:   `select * from "t" where "score" > (select "v" from "avgs" where "k" = $1)`,
			my:   "select * from `t` where `score` > (select `v` from `avgs` where `k` = ?)",
			lite: `select * from "t" where "score" > (select "v" from "avgs" where "k" = ?)`,
			args: []any{7},
		},
		{
			name: "integer in raw",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereIntegerInRaw("id", []string{"1", "2"}).OrWhereIntegerNotInRaw("n", []int64{3}).WhereIntegerInRaw("e", []int{})
			},
			pg:   `select * from "t" where "id" in (1, 2) or "n" not in (3) and 0 = 1`,
			my:   "select * from `t` where `id` in (1, 2) or `n` not in (3) and 0 = 1",
			lite: `select * from "t" where "id" in (1, 2) or "n" not in (3) and 0 = 1`,
		},
		{
			name: "null variants",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereNull("a").OrWhereNotNull("b").WhereNotNull("c").OrWhereNull("d")
			},
			pg:   `select * from "t" where "a" is null or "b" is not null and "c" is not null or "d" is null`,
			my:   "select * from `t` where `a` is null or `b` is not null and `c` is not null or `d` is null",
			lite: `select * from "t" where "a" is null or "b" is not null and "c" is not null or "d" is null`,
		},
		{
			name: "between family",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).
					WhereBetween("a", 1, 5).
					OrWhereNotBetween("b", 2, 3).
					WhereBetweenColumns("c", "lo", "hi").
					WhereNotBetweenColumns("d", "lo", "hi").
					WhereValueBetween(10, "min", "max").
					OrWhereValueNotBetween(11, "min", "max")
			},
			pg:   `select * from "t" where "a" between $1 and $2 or "b" not between $3 and $4 and "c" between "lo" and "hi" and "d" not between "lo" and "hi" and $5 between "min" and "max" or $6 not between "min" and "max"`,
			my:   "select * from `t` where `a` between ? and ? or `b` not between ? and ? and `c` between `lo` and `hi` and `d` not between `lo` and `hi` and ? between `min` and `max` or ? not between `min` and `max`",
			lite: `select * from "t" where "a" between ? and ? or "b" not between ? and ? and "c" between "lo" and "hi" and "d" not between "lo" and "hi" and ? between "min" and "max" or ? not between "min" and "max"`,
			args: []any{1, 5, 2, 3, 10, 11},
		},
		{
			name: "like case insensitive and sensitive per dialect",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereLike("a", "x%").WhereNotLike("b", "y_", true).OrWhereLike("c", "*?%_", true)
			},
			pg:   `select * from "t" where "a"::text ilike $1 and "b"::text not like $2 or "c"::text like $3`,
			my:   "select * from `t` where `a` like ? and `b` not like binary ? or `c` like binary ?",
			lite: `select * from "t" where "a" like ? and "b" not glob ? or "c" glob ?`,
			args: nil, // checked separately: sqlite converts case-sensitive patterns
		},
		{
			name: "basic like gets ::text on postgres",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).Where("name", "like", "a%").Where("age", ">", 1)
			},
			pg:   `select * from "t" where "name"::text like $1 and "age" > $2`,
			my:   "select * from `t` where `name` like ? and `age` > ?",
			lite: `select * from "t" where "name" like ? and "age" > ?`,
			args: []any{"a%", 1},
		},
		{
			name: "null safe equals",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereNullSafeEquals("a", 1).OrWhereNullSafeEquals("b", 2)
			},
			pg:   `select * from "t" where "a" is not distinct from $1 or "b" is not distinct from $2`,
			my:   "select * from `t` where `a` <=> ? or `b` <=> ?",
			lite: `select * from "t" where "a" is ? or "b" is ?`,
			args: []any{1, 2},
		},
		{
			name: "group nesting and empty group skipped",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).
					Where("a", 1).
					WhereGroup(func(q *db.Builder) {
						q.Where("b", 2).OrWhereGroup(func(q *db.Builder) { q.WhereNull("c").Where("d", 3) })
					}).
					OrWhereGroup(func(q *db.Builder) {}).
					OrWhereGroup(func(q *db.Builder) { q.WhereIn("e", []int{4}) })
			},
			pg:   `select * from "t" where "a" = $1 and ("b" = $2 or ("c" is null and "d" = $3)) or ("e" in ($4))`,
			my:   "select * from `t` where `a` = ? and (`b` = ? or (`c` is null and `d` = ?)) or (`e` in (?))",
			lite: `select * from "t" where "a" = ? and ("b" = ? or ("c" is null and "d" = ?)) or ("e" in (?))`,
			args: []any{1, 2, 3, 4},
		},
		{
			name: "where map sorted",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereMap(map[string]any{"b": 2, "a": 1, "c": nil})
			},
			pg:   `select * from "t" where ("a" = $1 and "b" = $2 and "c" is null)`,
			my:   "select * from `t` where (`a` = ? and `b` = ? and `c` is null)",
			lite: `select * from "t" where ("a" = ? and "b" = ? and "c" is null)`,
			args: []any{1, 2},
		},
		{
			name: "exists and not exists",
			build: func(d db.Dialect) *db.Builder {
				b := newT(d)
				return b.Where("a", 1).
					WhereExists(b.NewQuery().From("o").Select("1").WhereColumn("o.uid", "t.id")).
					WhereNotExists(b.NewQuery().From("p").Where("x", 2)).
					OrWhereNotExists(b.NewQuery().From("q"))
			},
			pg:   `select * from "t" where "a" = $1 and exists (select "1" from "o" where "o"."uid" = "t"."id") and not exists (select * from "p" where "x" = $2) or not exists (select * from "q")`,
			my:   "select * from `t` where `a` = ? and exists (select `1` from `o` where `o`.`uid` = `t`.`id`) and not exists (select * from `p` where `x` = ?) or not exists (select * from `q`)",
			lite: `select * from "t" where "a" = ? and exists (select "1" from "o" where "o"."uid" = "t"."id") and not exists (select * from "p" where "x" = ?) or not exists (select * from "q")`,
			args: []any{1, 2},
		},
		{
			name: "date parts with time.Time and padding",
			build: func(d db.Dialect) *db.Builder {
				at := time.Date(2024, 3, 7, 9, 5, 2, 0, time.UTC)
				return newT(d).
					WhereDate("c", at).
					WhereTime("c", ">=", at).
					WhereDay("c", 7).
					WhereMonth("c", "<", at).
					WhereYear("c", 2024)
			},
			pg:   `select * from "t" where "c"::date = $1 and "c"::time >= $2 and extract(day from "c") = $3 and extract(month from "c") < $4 and extract(year from "c") = $5`,
			my:   "select * from `t` where date(`c`) = ? and time(`c`) >= ? and day(`c`) = ? and month(`c`) < ? and year(`c`) = ?",
			lite: `select * from "t" where strftime('%Y-%m-%d', "c") = cast(? as text) and strftime('%H:%M:%S', "c") >= cast(? as text) and strftime('%d', "c") = cast(? as text) and strftime('%m', "c") < cast(? as text) and strftime('%Y', "c") = cast(? as text)`,
			args: []any{"2024-03-07", "09:05:02", "07", "03", 2024},
		},
		{
			name: "all any none",
			build: func(d db.Dialect) *db.Builder {
				cols := []string{"a", "b"}
				return newT(d).WhereAll(cols, ">", 1).WhereAny(cols, "x").WhereNone(cols, "like", "z%")
			},
			pg:   `select * from "t" where ("a" > $1 and "b" > $2) and ("a" = $3 or "b" = $4) and not ("a"::text like $5 or "b"::text like $6)`,
			my:   "select * from `t` where (`a` > ? and `b` > ?) and (`a` = ? or `b` = ?) and not (`a` like ? or `b` like ?)",
			lite: `select * from "t" where ("a" > ? and "b" > ?) and ("a" = ? or "b" = ?) and not ("a" like ? or "b" like ?)`,
			args: []any{1, 1, "x", "x", "z%", "z%"},
		},
		{
			name: "row values",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereRowValues([]string{"a", "b"}, "=", []any{1, 2}).OrWhereRowValues([]string{"c"}, ">", []any{3})
			},
			pg:   `select * from "t" where ("a", "b") = ($1, $2) or ("c") > ($3)`,
			my:   "select * from `t` where (`a`, `b`) = (?, ?) or (`c`) > (?)",
			lite: `select * from "t" where ("a", "b") = (?, ?) or ("c") > (?)`,
			args: []any{1, 2, 3},
		},
		{
			name: "having family and group",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).GroupBy("g").
					HavingNull("a").OrHavingNotNull("b").
					HavingBetween("c", 1, 2).OrHavingNotBetween("d", 3, 4).
					HavingGroup(func(q *db.Builder) { q.Having("e", ">", 5).OrHavingRaw("count(*) > ?", 6) })
			},
			pg:   `select * from "t" group by "g" having "a" is null or "b" is not null and "c" between $1 and $2 or "d" not between $3 and $4 and ("e" > $5 or count(*) > $6)`,
			my:   "select * from `t` group by `g` having `a` is null or `b` is not null and `c` between ? and ? or `d` not between ? and ? and (`e` > ? or count(*) > ?)",
			lite: `select * from "t" group by "g" having "a" is null or "b" is not null and "c" between ? and ? or "d" not between ? and ? and ("e" > ? or count(*) > ?)`,
			args: []any{1, 2, 3, 4, 5, 6},
		},
	}
	// the like case needs per-dialect args, so run it separately.
	var likeCase golden
	rest := tests[:0]
	for _, tc := range tests {
		if tc.name == "like case insensitive and sensitive per dialect" {
			likeCase = tc
			continue
		}
		rest = append(rest, tc)
	}
	runGolden(t, rest)

	for _, c := range []struct {
		d    db.Dialect
		want string
		args []any
	}{
		{db.Postgres, likeCase.pg, []any{"x%", "y_", "*?%_"}},
		{db.MySQL, likeCase.my, []any{"x%", "y_", "*?%_"}},
		{db.SQLite, likeCase.lite, []any{"x%", "y?", "[*][?]*?"}},
	} {
		t.Run("like/"+c.d.String(), func(t *testing.T) {
			sql, args, err := likeCase.build(c.d).ToSQL()
			require.NoError(t, err)
			assert.Equal(t, c.want, sql)
			assert.Equal(t, len(c.args), len(args))
			for i := range args {
				assert.Equal(t, c.args[i], args[i])
			}
		})
	}
}

func TestWhereFamilyErrors(t *testing.T) {
	tests := []struct {
		name  string
		build *db.Builder
		want  error
	}{
		{"nil with ordering op", newT(db.Postgres).Where("a", ">", nil), db.ErrInvalidArgs},
		{"integer in raw rejects text", newT(db.Postgres).WhereIntegerInRaw("id", []string{"1; drop"}), db.ErrInvalidArgs},
		{"date nil", newT(db.Postgres).WhereDate("c", nil), db.ErrInvalidArgs},
		{"day not numeric", newT(db.Postgres).WhereDay("c", "x"), db.ErrInvalidArgs},
		{"column second not string", newT(db.Postgres).WhereColumn("a", 5), db.ErrInvalidArgs},
		{"row values mismatch", newT(db.Postgres).WhereRowValues([]string{"a", "b"}, "=", []any{1}), db.ErrInvalidArgs},
		{"row values bad op", newT(db.Postgres).WhereRowValues([]string{"a"}, "; --", []any{1}), db.ErrInvalidOperator},
		{"bad op inside group propagates", newT(db.Postgres).WhereGroup(func(q *db.Builder) { q.Where("a", "bogus", 1) }), db.ErrInvalidOperator},
		{"subquery error propagates", newT(db.Postgres).WhereIn("id", db.NewBuilder(db.Postgres)), db.ErrNoTable},
		{"all bad op", newT(db.Postgres).WhereAll([]string{"a"}, "bogus", 1), db.ErrInvalidOperator},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestSubquerySnapshotSemantics(t *testing.T) {
	sub := db.Use(db.Postgres).Table("s").Where("a", 1)
	base := newT(db.Postgres).WhereIn("id", sub)
	c := base.Clone()
	sub.Where("b", 2) // mutating the caller's builder after use must not leak in
	got, _, _ := c.ToSQL()
	if want := `select * from "t" where "id" in (select * from "s" where "a" = $1)`; got != want {
		t.Errorf("clone shared sub:\n got: %s\nwant: %s", got, want)
	}
}

// TestEveryWhereVariantUsesTheRightConnective appends each predicate method after
// `where a = $1` and checks the connective that was emitted.
func TestEveryWhereVariantUsesTheRightConnective(t *testing.T) {
	d := db.Postgres
	sub := func() *db.Builder { return db.Use(d).Table("s") }
	cols := []string{"x", "y"}
	type variant struct {
		name string
		add  func(*db.Builder) *db.Builder
		want string // connective expected between `"a" = $1` and the new clause
	}
	vs := []variant{
		{"OrWhere", func(b *db.Builder) *db.Builder { return b.OrWhere("c", 1) }, " or "},
		{"OrWhereNot", func(b *db.Builder) *db.Builder { return b.OrWhereNot("c", 1) }, " or not "},
		{"WhereNot", func(b *db.Builder) *db.Builder { return b.WhereNot("c", 1) }, " and not "},
		{"OrWhereColumn", func(b *db.Builder) *db.Builder { return b.OrWhereColumn("c", "d") }, " or "},
		{"OrWhereIn", func(b *db.Builder) *db.Builder { return b.OrWhereIn("c", []int{1}) }, " or "},
		{"OrWhereNotIn", func(b *db.Builder) *db.Builder { return b.OrWhereNotIn("c", []int{1}) }, " or "},
		{"WhereIntegerNotInRaw", func(b *db.Builder) *db.Builder { return b.WhereIntegerNotInRaw("c", []int{1}) }, " and "},
		{"OrWhereIntegerInRaw", func(b *db.Builder) *db.Builder { return b.OrWhereIntegerInRaw("c", []int{1}) }, " or "},
		{"OrWhereIntegerNotInRaw", func(b *db.Builder) *db.Builder { return b.OrWhereIntegerNotInRaw("c", []int{1}) }, " or "},
		{"OrWhereNull", func(b *db.Builder) *db.Builder { return b.OrWhereNull("c") }, " or "},
		{"OrWhereNotNull", func(b *db.Builder) *db.Builder { return b.OrWhereNotNull("c") }, " or "},
		{"WhereNotBetween", func(b *db.Builder) *db.Builder { return b.WhereNotBetween("c", 1, 2) }, " and "},
		{"OrWhereBetween", func(b *db.Builder) *db.Builder { return b.OrWhereBetween("c", 1, 2) }, " or "},
		{"OrWhereNotBetween", func(b *db.Builder) *db.Builder { return b.OrWhereNotBetween("c", 1, 2) }, " or "},
		{"OrWhereBetweenColumns", func(b *db.Builder) *db.Builder { return b.OrWhereBetweenColumns("c", "l", "h") }, " or "},
		{"OrWhereNotBetweenColumns", func(b *db.Builder) *db.Builder { return b.OrWhereNotBetweenColumns("c", "l", "h") }, " or "},
		{"OrWhereValueBetween", func(b *db.Builder) *db.Builder { return b.OrWhereValueBetween(1, "l", "h") }, " or "},
		{"WhereValueNotBetween", func(b *db.Builder) *db.Builder { return b.WhereValueNotBetween(1, "l", "h") }, " and "},
		{"OrWhereLike", func(b *db.Builder) *db.Builder { return b.OrWhereLike("c", "x") }, " or "},
		{"OrWhereNotLike", func(b *db.Builder) *db.Builder { return b.OrWhereNotLike("c", "x") }, " or "},
		{"OrWhereNullSafeEquals", func(b *db.Builder) *db.Builder { return b.OrWhereNullSafeEquals("c", 1) }, " or "},
		{"OrWhereGroup", func(b *db.Builder) *db.Builder {
			return b.OrWhereGroup(func(q *db.Builder) { q.Where("c", 1) })
		}, " or "},
		{"OrWhereMap", func(b *db.Builder) *db.Builder { return b.OrWhereMap(m("c", 1)) }, " or "},
		{"OrWhereExists", func(b *db.Builder) *db.Builder { return b.OrWhereExists(sub()) }, " or "},
		{"WhereNotExists", func(b *db.Builder) *db.Builder { return b.WhereNotExists(sub()) }, " and not "},
		{"OrWhereNotExists", func(b *db.Builder) *db.Builder { return b.OrWhereNotExists(sub()) }, " or not "},
		{"OrWhereDate", func(b *db.Builder) *db.Builder { return b.OrWhereDate("c", "2024-01-01") }, " or "},
		{"OrWhereTime", func(b *db.Builder) *db.Builder { return b.OrWhereTime("c", "01:00:00") }, " or "},
		{"OrWhereDay", func(b *db.Builder) *db.Builder { return b.OrWhereDay("c", 1) }, " or "},
		{"OrWhereMonth", func(b *db.Builder) *db.Builder { return b.OrWhereMonth("c", 1) }, " or "},
		{"OrWhereYear", func(b *db.Builder) *db.Builder { return b.OrWhereYear("c", 2024) }, " or "},
		{"OrWhereAll", func(b *db.Builder) *db.Builder { return b.OrWhereAll(cols, 1) }, " or "},
		{"OrWhereAny", func(b *db.Builder) *db.Builder { return b.OrWhereAny(cols, 1) }, " or "},
		{"OrWhereNone", func(b *db.Builder) *db.Builder { return b.OrWhereNone(cols, 1) }, " or not "},
		{"OrWhereRowValues", func(b *db.Builder) *db.Builder { return b.OrWhereRowValues(cols, "=", []any{1, 2}) }, " or "},
		{"OrWhereJsonContains", func(b *db.Builder) *db.Builder { return b.OrWhereJsonContains("o->k", 1) }, " or "},
		{"OrWhereJsonDoesntContain", func(b *db.Builder) *db.Builder { return b.OrWhereJsonDoesntContain("o->k", 1) }, " or not "},
		{"OrWhereJsonContainsKey", func(b *db.Builder) *db.Builder { return b.OrWhereJsonContainsKey("o->k") }, " or "},
		{"OrWhereJsonDoesntContainKey", func(b *db.Builder) *db.Builder { return b.OrWhereJsonDoesntContainKey("o->k") }, " or not "},
		{"OrWhereJsonLength", func(b *db.Builder) *db.Builder { return b.OrWhereJsonLength("o->k", 1) }, " or "},
	}
	for _, v := range vs {
		t.Run(v.name, func(t *testing.T) {
			sql, _, err := v.add(db.Use(d).Table("t").Where("a", 1)).ToSQL()
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(sql, `select * from "t" where "a" = $1`+v.want))
		})
	}

	havings := []struct {
		name string
		add  func(*db.Builder) *db.Builder
		want string
	}{
		{"OrHavingNull", func(b *db.Builder) *db.Builder { return b.OrHavingNull("c") }, " or "},
		{"HavingNotNull", func(b *db.Builder) *db.Builder { return b.HavingNotNull("c") }, " and "},
		{"OrHavingBetween", func(b *db.Builder) *db.Builder { return b.OrHavingBetween("c", 1, 2) }, " or "},
		{"HavingNotBetween", func(b *db.Builder) *db.Builder { return b.HavingNotBetween("c", 1, 2) }, " and "},
		{"OrHavingGroup", func(b *db.Builder) *db.Builder {
			return b.OrHavingGroup(func(q *db.Builder) { q.Having("c", 1) })
		}, " or "},
		{"OrHavingRaw", func(b *db.Builder) *db.Builder { return b.OrHavingRaw("c = ?", 1) }, " or "},
	}
	for _, v := range havings {
		t.Run(v.name, func(t *testing.T) {
			sql, _, err := v.add(db.Use(d).Table("t").GroupBy("g").Having("a", 1)).ToSQL()
			require.NoError(t, err)
			assert.Contains(t, sql, `having "a" = $1`+v.want)
		})
	}
}
