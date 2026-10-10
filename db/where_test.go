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
			pg:   `SELECT * FROM "t" WHERE NOT "a" = $1 OR NOT "b" > $2 AND NOT "c" IS NULL`,
			my:   "SELECT * FROM `t` WHERE NOT `a` = ? OR NOT `b` > ? AND NOT `c` IS NULL",
			lite: `SELECT * FROM "t" WHERE NOT "a" = ? OR NOT "b" > ? AND NOT "c" IS NULL`,
			args: []any{1, 2},
		},
		{
			name: "where column",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereColumn("a.x", "b.y").OrWhereColumn("first", "<>", "last")
			},
			pg:   `SELECT * FROM "t" WHERE "a"."x" = "b"."y" OR "first" <> "last"`,
			my:   "SELECT * FROM `t` WHERE `a`.`x` = `b`.`y` OR `first` <> `last`",
			lite: `SELECT * FROM "t" WHERE "a"."x" = "b"."y" OR "first" <> "last"`,
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
			pg:   `SELECT * FROM "t" WHERE "id" IN ($1, $2, $3) OR "s" IN ($4) AND "n" NOT IN ($5, $6) AND 0 = 1 AND 1 = 1`,
			my:   "SELECT * FROM `t` WHERE `id` IN (?, ?, ?) OR `s` IN (?) AND `n` NOT IN (?, ?) AND 0 = 1 AND 1 = 1",
			lite: `SELECT * FROM "t" WHERE "id" IN (?, ?, ?) OR "s" IN (?) AND "n" NOT IN (?, ?) AND 0 = 1 AND 1 = 1`,
			args: []any{1, 2, 3, "a", 4, 5},
		},
		{
			name: "in subquery shares arg numbering",
			build: func(d db.Dialect) *db.Builder {
				sub := db.Use(d).Table("bans").Select("user_id").Where("reason", "spam").Where("level", ">", 2)
				return newT(d).Where("a", 1).WhereIn("id", sub).Where("z", 9)
			},
			pg:   `SELECT * FROM "t" WHERE "a" = $1 AND "id" IN (SELECT "user_id" FROM "bans" WHERE "reason" = $2 AND "level" > $3) AND "z" = $4`,
			my:   "SELECT * FROM `t` WHERE `a` = ? AND `id` IN (SELECT `user_id` FROM `bans` WHERE `reason` = ? AND `level` > ?) AND `z` = ?",
			lite: `SELECT * FROM "t" WHERE "a" = ? AND "id" IN (SELECT "user_id" FROM "bans" WHERE "reason" = ? AND "level" > ?) AND "z" = ?`,
			args: []any{1, "spam", 2, 9},
		},
		{
			name: "scalar subquery value",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).Where("score", ">", db.Use(d).Table("avgs").Select("v").Where("k", 7))
			},
			pg:   `SELECT * FROM "t" WHERE "score" > (SELECT "v" FROM "avgs" WHERE "k" = $1)`,
			my:   "SELECT * FROM `t` WHERE `score` > (SELECT `v` FROM `avgs` WHERE `k` = ?)",
			lite: `SELECT * FROM "t" WHERE "score" > (SELECT "v" FROM "avgs" WHERE "k" = ?)`,
			args: []any{7},
		},
		{
			name: "integer in raw",
			build: func(d db.Dialect) *db.Builder {
				return newT(
					d,
				).WhereIntegerInRaw("id", []string{"1", "2"}).
					OrWhereIntegerNotInRaw("n", []int64{3}).
					WhereIntegerInRaw("e", []int{})
			},
			pg:   `SELECT * FROM "t" WHERE "id" IN (1, 2) OR "n" NOT IN (3) AND 0 = 1`,
			my:   "SELECT * FROM `t` WHERE `id` IN (1, 2) OR `n` NOT IN (3) AND 0 = 1",
			lite: `SELECT * FROM "t" WHERE "id" IN (1, 2) OR "n" NOT IN (3) AND 0 = 1`,
		},
		{
			name: "null variants",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereNull("a").OrWhereNotNull("b").WhereNotNull("c").OrWhereNull("d")
			},
			pg:   `SELECT * FROM "t" WHERE "a" IS NULL OR "b" IS NOT NULL AND "c" IS NOT NULL OR "d" IS NULL`,
			my:   "SELECT * FROM `t` WHERE `a` IS NULL OR `b` IS NOT NULL AND `c` IS NOT NULL OR `d` IS NULL",
			lite: `SELECT * FROM "t" WHERE "a" IS NULL OR "b" IS NOT NULL AND "c" IS NOT NULL OR "d" IS NULL`,
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
			pg:   `SELECT * FROM "t" WHERE "a" BETWEEN $1 AND $2 OR "b" NOT BETWEEN $3 AND $4 AND "c" BETWEEN "lo" AND "hi" AND "d" NOT BETWEEN "lo" AND "hi" AND $5 BETWEEN "min" AND "max" OR $6 NOT BETWEEN "min" AND "max"`,
			my:   "SELECT * FROM `t` WHERE `a` BETWEEN ? AND ? OR `b` NOT BETWEEN ? AND ? AND `c` BETWEEN `lo` AND `hi` AND `d` NOT BETWEEN `lo` AND `hi` AND ? BETWEEN `min` AND `max` OR ? NOT BETWEEN `min` AND `max`",
			lite: `SELECT * FROM "t" WHERE "a" BETWEEN ? AND ? OR "b" NOT BETWEEN ? AND ? AND "c" BETWEEN "lo" AND "hi" AND "d" NOT BETWEEN "lo" AND "hi" AND ? BETWEEN "min" AND "max" OR ? NOT BETWEEN "min" AND "max"`,
			args: []any{1, 5, 2, 3, 10, 11},
		},
		{
			name: "like case insensitive and sensitive per dialect",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereLike("a", "x%").WhereNotLike("b", "y_", true).OrWhereLike("c", "*?%_", true)
			},
			pg:   `SELECT * FROM "t" WHERE "a"::TEXT ILIKE $1 AND "b"::TEXT NOT LIKE $2 OR "c"::TEXT LIKE $3`,
			my:   "SELECT * FROM `t` WHERE `a` LIKE ? AND `b` NOT LIKE BINARY ? OR `c` LIKE BINARY ?",
			lite: `SELECT * FROM "t" WHERE "a" LIKE ? AND "b" NOT GLOB ? OR "c" GLOB ?`,
			args: nil, // checked separately: sqlite converts case-sensitive patterns
		},
		{
			name: "basic like gets ::text on postgres",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).Where("name", "like", "a%").Where("age", ">", 1)
			},
			pg:   `SELECT * FROM "t" WHERE "name"::TEXT LIKE $1 AND "age" > $2`,
			my:   "SELECT * FROM `t` WHERE `name` LIKE ? AND `age` > ?",
			lite: `SELECT * FROM "t" WHERE "name" LIKE ? AND "age" > ?`,
			args: []any{"a%", 1},
		},
		{
			name: "null safe equals",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereNullSafeEquals("a", 1).OrWhereNullSafeEquals("b", 2)
			},
			pg:   `SELECT * FROM "t" WHERE "a" IS NOT DISTINCT FROM $1 OR "b" IS NOT DISTINCT FROM $2`,
			my:   "SELECT * FROM `t` WHERE `a` <=> ? OR `b` <=> ?",
			lite: `SELECT * FROM "t" WHERE "a" IS ? OR "b" IS ?`,
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
					OrWhereGroup(func(*db.Builder) {}).
					OrWhereGroup(func(q *db.Builder) { q.WhereIn("e", []int{4}) })
			},
			pg:   `SELECT * FROM "t" WHERE "a" = $1 AND ("b" = $2 OR ("c" IS NULL AND "d" = $3)) OR ("e" IN ($4))`,
			my:   "SELECT * FROM `t` WHERE `a` = ? AND (`b` = ? OR (`c` IS NULL AND `d` = ?)) OR (`e` IN (?))",
			lite: `SELECT * FROM "t" WHERE "a" = ? AND ("b" = ? OR ("c" IS NULL AND "d" = ?)) OR ("e" IN (?))`,
			args: []any{1, 2, 3, 4},
		},
		{
			name: "where map sorted",
			build: func(d db.Dialect) *db.Builder {
				return newT(d).WhereMap(map[string]any{"b": 2, "a": 1, "c": nil})
			},
			pg:   `SELECT * FROM "t" WHERE ("a" = $1 AND "b" = $2 AND "c" IS NULL)`,
			my:   "SELECT * FROM `t` WHERE (`a` = ? AND `b` = ? AND `c` IS NULL)",
			lite: `SELECT * FROM "t" WHERE ("a" = ? AND "b" = ? AND "c" IS NULL)`,
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
			pg:   `SELECT * FROM "t" WHERE "a" = $1 AND EXISTS (SELECT "1" FROM "o" WHERE "o"."uid" = "t"."id") AND NOT EXISTS (SELECT * FROM "p" WHERE "x" = $2) OR NOT EXISTS (SELECT * FROM "q")`,
			my:   "SELECT * FROM `t` WHERE `a` = ? AND EXISTS (SELECT `1` FROM `o` WHERE `o`.`uid` = `t`.`id`) AND NOT EXISTS (SELECT * FROM `p` WHERE `x` = ?) OR NOT EXISTS (SELECT * FROM `q`)",
			lite: `SELECT * FROM "t" WHERE "a" = ? AND EXISTS (SELECT "1" FROM "o" WHERE "o"."uid" = "t"."id") AND NOT EXISTS (SELECT * FROM "p" WHERE "x" = ?) OR NOT EXISTS (SELECT * FROM "q")`,
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
			pg:   `SELECT * FROM "t" WHERE "c"::DATE = $1 AND "c"::TIME >= $2 AND EXTRACT(DAY FROM "c") = $3 AND EXTRACT(MONTH FROM "c") < $4 AND EXTRACT(YEAR FROM "c") = $5`,
			my:   "SELECT * FROM `t` WHERE DATE(`c`) = ? AND TIME(`c`) >= ? AND DAY(`c`) = ? AND MONTH(`c`) < ? AND YEAR(`c`) = ?",
			lite: `SELECT * FROM "t" WHERE STRFTIME('%Y-%m-%d', "c") = CAST(? AS TEXT) AND STRFTIME('%H:%M:%S', "c") >= CAST(? AS TEXT) AND STRFTIME('%d', "c") = CAST(? AS TEXT) AND STRFTIME('%m', "c") < CAST(? AS TEXT) AND STRFTIME('%Y', "c") = CAST(? AS TEXT)`,
			args: []any{"2024-03-07", "09:05:02", "07", "03", 2024},
		},
		{
			name: "all any none",
			build: func(d db.Dialect) *db.Builder {
				cols := []string{"a", "b"}
				return newT(d).WhereAll(cols, ">", 1).WhereAny(cols, "x").WhereNone(cols, "like", "z%")
			},
			pg:   `SELECT * FROM "t" WHERE ("a" > $1 AND "b" > $2) AND ("a" = $3 OR "b" = $4) AND NOT ("a"::TEXT LIKE $5 OR "b"::TEXT LIKE $6)`,
			my:   "SELECT * FROM `t` WHERE (`a` > ? AND `b` > ?) AND (`a` = ? OR `b` = ?) AND NOT (`a` LIKE ? OR `b` LIKE ?)",
			lite: `SELECT * FROM "t" WHERE ("a" > ? AND "b" > ?) AND ("a" = ? OR "b" = ?) AND NOT ("a" LIKE ? OR "b" LIKE ?)`,
			args: []any{1, 1, "x", "x", "z%", "z%"},
		},
		{
			name: "row values",
			build: func(d db.Dialect) *db.Builder {
				return newT(
					d,
				).WhereRowValues([]string{"a", "b"}, "=", []any{1, 2}).
					OrWhereRowValues([]string{"c"}, ">", []any{3})
			},
			pg:   `SELECT * FROM "t" WHERE ("a", "b") = ($1, $2) OR ("c") > ($3)`,
			my:   "SELECT * FROM `t` WHERE (`a`, `b`) = (?, ?) OR (`c`) > (?)",
			lite: `SELECT * FROM "t" WHERE ("a", "b") = (?, ?) OR ("c") > (?)`,
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
			pg:   `SELECT * FROM "t" GROUP BY "g" HAVING "a" IS NULL OR "b" IS NOT NULL AND "c" BETWEEN $1 AND $2 OR "d" NOT BETWEEN $3 AND $4 AND ("e" > $5 OR count(*) > $6)`,
			my:   "SELECT * FROM `t` GROUP BY `g` HAVING `a` IS NULL OR `b` IS NOT NULL AND `c` BETWEEN ? AND ? OR `d` NOT BETWEEN ? AND ? AND (`e` > ? OR count(*) > ?)",
			lite: `SELECT * FROM "t" GROUP BY "g" HAVING "a" IS NULL OR "b" IS NOT NULL AND "c" BETWEEN ? AND ? OR "d" NOT BETWEEN ? AND ? AND ("e" > ? OR count(*) > ?)`,
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
			assert.Len(t, args, len(c.args))
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
		{"nil WITH ordering op", newT(db.Postgres).Where("a", ">", nil), db.ErrInvalidArgs},
		{
			"integer in raw rejects text",
			newT(db.Postgres).WhereIntegerInRaw("id", []string{"1; drop"}),
			db.ErrInvalidArgs,
		},
		{"date nil", newT(db.Postgres).WhereDate("c", nil), db.ErrInvalidArgs},
		{"day not numeric", newT(db.Postgres).WhereDay("c", "x"), db.ErrInvalidArgs},
		{"column second not string", newT(db.Postgres).WhereColumn("a", 5), db.ErrInvalidArgs},
		{"row VALUES mismatch", newT(db.Postgres).WhereRowValues([]string{"a", "b"}, "=", []any{1}), db.ErrInvalidArgs},
		{"row VALUES bad op", newT(db.Postgres).WhereRowValues([]string{"a"}, "; --", []any{1}), db.ErrInvalidOperator},
		{
			"bad op inside group propagates",
			newT(db.Postgres).WhereGroup(func(q *db.Builder) { q.Where("a", "bogus", 1) }),
			db.ErrInvalidOperator,
		},
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
	if want := `SELECT * FROM "t" WHERE "id" IN (SELECT * FROM "s" WHERE "a" = $1)`; got != want {
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
		{"OrWhere", func(b *db.Builder) *db.Builder { return b.OrWhere("c", 1) }, " OR "},
		{"OrWhereNot", func(b *db.Builder) *db.Builder { return b.OrWhereNot("c", 1) }, " OR NOT "},
		{"WhereNot", func(b *db.Builder) *db.Builder { return b.WhereNot("c", 1) }, " AND NOT "},
		{"OrWhereColumn", func(b *db.Builder) *db.Builder { return b.OrWhereColumn("c", "d") }, " OR "},
		{"OrWhereIn", func(b *db.Builder) *db.Builder { return b.OrWhereIn("c", []int{1}) }, " OR "},
		{"OrWhereNotIn", func(b *db.Builder) *db.Builder { return b.OrWhereNotIn("c", []int{1}) }, " OR "},
		{
			"WhereIntegerNotInRaw",
			func(b *db.Builder) *db.Builder { return b.WhereIntegerNotInRaw("c", []int{1}) },
			" AND ",
		},
		{
			"OrWhereIntegerInRaw",
			func(b *db.Builder) *db.Builder { return b.OrWhereIntegerInRaw("c", []int{1}) },
			" OR ",
		},
		{
			"OrWhereIntegerNotInRaw",
			func(b *db.Builder) *db.Builder { return b.OrWhereIntegerNotInRaw("c", []int{1}) },
			" OR ",
		},
		{"OrWhereNull", func(b *db.Builder) *db.Builder { return b.OrWhereNull("c") }, " OR "},
		{"OrWhereNotNull", func(b *db.Builder) *db.Builder { return b.OrWhereNotNull("c") }, " OR "},
		{"WhereNotBetween", func(b *db.Builder) *db.Builder { return b.WhereNotBetween("c", 1, 2) }, " AND "},
		{"OrWhereBetween", func(b *db.Builder) *db.Builder { return b.OrWhereBetween("c", 1, 2) }, " OR "},
		{"OrWhereNotBetween", func(b *db.Builder) *db.Builder { return b.OrWhereNotBetween("c", 1, 2) }, " OR "},
		{
			"OrWhereBetweenColumns",
			func(b *db.Builder) *db.Builder { return b.OrWhereBetweenColumns("c", "l", "h") },
			" OR ",
		},
		{
			"OrWhereNotBetweenColumns",
			func(b *db.Builder) *db.Builder { return b.OrWhereNotBetweenColumns("c", "l", "h") },
			" OR ",
		},
		{"OrWhereValueBetween", func(b *db.Builder) *db.Builder { return b.OrWhereValueBetween(1, "l", "h") }, " OR "},
		{
			"WhereValueNotBetween",
			func(b *db.Builder) *db.Builder { return b.WhereValueNotBetween(1, "l", "h") },
			" AND ",
		},
		{"OrWhereLike", func(b *db.Builder) *db.Builder { return b.OrWhereLike("c", "x") }, " OR "},
		{"OrWhereNotLike", func(b *db.Builder) *db.Builder { return b.OrWhereNotLike("c", "x") }, " OR "},
		{"OrWhereNullSafeEquals", func(b *db.Builder) *db.Builder { return b.OrWhereNullSafeEquals("c", 1) }, " OR "},
		{"OrWhereGroup", func(b *db.Builder) *db.Builder {
			return b.OrWhereGroup(func(q *db.Builder) { q.Where("c", 1) })
		}, " OR "},
		{"OrWhereMap", func(b *db.Builder) *db.Builder { return b.OrWhereMap(m("c", 1)) }, " OR "},
		{"OrWhereExists", func(b *db.Builder) *db.Builder { return b.OrWhereExists(sub()) }, " OR "},
		{"WhereNotExists", func(b *db.Builder) *db.Builder { return b.WhereNotExists(sub()) }, " AND NOT "},
		{"OrWhereNotExists", func(b *db.Builder) *db.Builder { return b.OrWhereNotExists(sub()) }, " OR NOT "},
		{"OrWhereDate", func(b *db.Builder) *db.Builder { return b.OrWhereDate("c", "2024-01-01") }, " OR "},
		{"OrWhereTime", func(b *db.Builder) *db.Builder { return b.OrWhereTime("c", "01:00:00") }, " OR "},
		{"OrWhereDay", func(b *db.Builder) *db.Builder { return b.OrWhereDay("c", 1) }, " OR "},
		{"OrWhereMonth", func(b *db.Builder) *db.Builder { return b.OrWhereMonth("c", 1) }, " OR "},
		{"OrWhereYear", func(b *db.Builder) *db.Builder { return b.OrWhereYear("c", 2024) }, " OR "},
		{"OrWhereAll", func(b *db.Builder) *db.Builder { return b.OrWhereAll(cols, 1) }, " OR "},
		{"OrWhereAny", func(b *db.Builder) *db.Builder { return b.OrWhereAny(cols, 1) }, " OR "},
		{"OrWhereNone", func(b *db.Builder) *db.Builder { return b.OrWhereNone(cols, 1) }, " OR NOT "},
		{
			"OrWhereRowValues",
			func(b *db.Builder) *db.Builder { return b.OrWhereRowValues(cols, "=", []any{1, 2}) },
			" OR ",
		},
		{"OrWhereJsonContains", func(b *db.Builder) *db.Builder { return b.OrWhereJsonContains("o->k", 1) }, " OR "},
		{
			"OrWhereJsonDoesntContain",
			func(b *db.Builder) *db.Builder { return b.OrWhereJsonDoesntContain("o->k", 1) },
			" OR NOT ",
		},
		{"OrWhereJsonContainsKey", func(b *db.Builder) *db.Builder { return b.OrWhereJsonContainsKey("o->k") }, " OR "},
		{
			"OrWhereJsonDoesntContainKey",
			func(b *db.Builder) *db.Builder { return b.OrWhereJsonDoesntContainKey("o->k") },
			" OR NOT ",
		},
		{"OrWhereJsonLength", func(b *db.Builder) *db.Builder { return b.OrWhereJsonLength("o->k", 1) }, " OR "},
	}
	for _, v := range vs {
		t.Run(v.name, func(t *testing.T) {
			sql, _, err := v.add(db.Use(d).Table("t").Where("a", 1)).ToSQL()
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(sql, `SELECT * FROM "t" WHERE "a" = $1`+v.want))
		})
	}

	havings := []struct {
		name string
		add  func(*db.Builder) *db.Builder
		want string
	}{
		{"OrHavingNull", func(b *db.Builder) *db.Builder { return b.OrHavingNull("c") }, " OR "},
		{"HavingNotNull", func(b *db.Builder) *db.Builder { return b.HavingNotNull("c") }, " AND "},
		{"OrHavingBetween", func(b *db.Builder) *db.Builder { return b.OrHavingBetween("c", 1, 2) }, " OR "},
		{"HavingNotBetween", func(b *db.Builder) *db.Builder { return b.HavingNotBetween("c", 1, 2) }, " AND "},
		{"OrHavingGroup", func(b *db.Builder) *db.Builder {
			return b.OrHavingGroup(func(q *db.Builder) { q.Having("c", 1) })
		}, " OR "},
		{"OrHavingRaw", func(b *db.Builder) *db.Builder { return b.OrHavingRaw("c = ?", 1) }, " OR "},
	}
	for _, v := range havings {
		t.Run(v.name, func(t *testing.T) {
			sql, _, err := v.add(db.Use(d).Table("t").GroupBy("g").Having("a", 1)).ToSQL()
			require.NoError(t, err)
			assert.Contains(t, sql, `HAVING "a" = $1`+v.want)
		})
	}
}

// WhereIn takes typed fast paths for common slices and falls back to reflection for the rest;
// both must bind the same arguments.
func TestWhereInSliceTypes(t *testing.T) {
	type ids []int
	cases := []struct {
		name string
		in   any
		want []any
	}{
		{"ints", []int{1, 2, 3}, []any{1, 2, 3}},
		{"int64s", []int64{1, 2}, []any{int64(1), int64(2)}},
		{"strings", []string{"a", "b"}, []any{"a", "b"}},
		{"anys", []any{1, "b"}, []any{1, "b"}},
		{"named slice type", ids{4, 5}, []any{4, 5}},
		{"int32s via reflection", []int32{6, 7}, []any{int32(6), int32(7)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, args, err := newT(db.Postgres).WhereIn("id", tc.in).ToSQL()
			require.NoError(t, err)
			assert.Equal(t, tc.want, args)
		})
	}
}
