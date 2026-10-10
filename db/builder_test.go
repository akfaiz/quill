package db_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

type golden struct {
	name  string
	build func(d db.Dialect) *db.Builder
	pg    string
	my    string
	lite  string
	maria string // MariaDB: same as my unless set
	args  []any
}

func TestSelectGolden(t *testing.T) {
	tests := []golden{
		{
			name:  "select all",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("users") },
			pg:    `SELECT * FROM "users"`,
			my:    "SELECT * FROM `users`",
			lite:  `SELECT * FROM "users"`,
		},
		{
			name: "columns alias distinct qualified",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("users as u").Distinct().Select("u.id", "u.name as n", "u.*")
			},
			pg:   `SELECT DISTINCT "u"."id", "u"."name" AS "n", "u".* FROM "users" AS "u"`,
			my:   "SELECT DISTINCT `u`.`id`, `u`.`name` AS `n`, `u`.* FROM `users` AS `u`",
			lite: `SELECT DISTINCT "u"."id", "u"."name" AS "n", "u".* FROM "users" AS "u"`,
		},
		{
			name: "where and or null",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("users").
					Where("votes", ">", 100).
					OrWhere("name", "john").
					Where("deleted_at", nil).
					Where("role", "!=", nil)
			},
			pg:   `SELECT * FROM "users" WHERE "votes" > $1 OR "name" = $2 AND "deleted_at" IS NULL AND "role" IS NOT NULL`,
			my:   "SELECT * FROM `users` WHERE `votes` > ? OR `name` = ? AND `deleted_at` IS NULL AND `role` IS NOT NULL",
			lite: `SELECT * FROM "users" WHERE "votes" > ? OR "name" = ? AND "deleted_at" IS NULL AND "role" IS NOT NULL`,
			args: []any{100, "john"},
		},
		{
			name: "raw keeps placeholders in order",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").
					SelectRaw("count(*) filter (WHERE x > ?) AS c", 5).
					WhereRaw("a = ? and b = '?' and c ?? 'k'", 1).
					Where("z", 9)
			},
			pg:   `SELECT count(*) filter (WHERE x > $1) AS c FROM "t" WHERE a = $2 and b = '?' and c ? 'k' AND "z" = $3`,
			my:   "SELECT count(*) filter (WHERE x > ?) AS c FROM `t` WHERE a = ? and b = '?' and c ? 'k' AND `z` = ?",
			lite: `SELECT count(*) filter (WHERE x > ?) AS c FROM "t" WHERE a = ? and b = '?' and c ? 'k' AND "z" = ?`,
			args: []any{5, 1, 9},
		},
		{
			name: "group having order limit offset",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("orders").
					Select("user_id").SelectRaw("sum(total) as s").
					GroupBy("user_id").
					Having("s", ">", 10).OrHavingRaw("count(*) > ?", 3).
					OrderBy("user_id").OrderByDesc("s").OrderByRaw("random()").
					Limit(10).Offset(20)
			},
			pg:   `SELECT "user_id", sum(total) as s FROM "orders" GROUP BY "user_id" HAVING "s" > $1 OR count(*) > $2 ORDER BY "user_id" ASC, "s" DESC, random() LIMIT 10 OFFSET 20`,
			my:   "SELECT `user_id`, sum(total) as s FROM `orders` GROUP BY `user_id` HAVING `s` > ? OR count(*) > ? ORDER BY `user_id` ASC, `s` DESC, random() LIMIT 10 OFFSET 20",
			lite: `SELECT "user_id", sum(total) as s FROM "orders" GROUP BY "user_id" HAVING "s" > ? OR count(*) > ? ORDER BY "user_id" ASC, "s" DESC, random() LIMIT 10 OFFSET 20`,
			args: []any{10, 3},
		},
		{
			name:  "for page",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").ForPage(3, 15) },
			pg:    `SELECT * FROM "t" LIMIT 15 OFFSET 30`,
			my:    "SELECT * FROM `t` LIMIT 15 OFFSET 30",
			lite:  `SELECT * FROM "t" LIMIT 15 OFFSET 30`,
		},
		{
			name:  "offset without limit",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").Skip(5) },
			pg:    `SELECT * FROM "t" OFFSET 5`,
			my:    "SELECT * FROM `t` LIMIT 18446744073709551615 OFFSET 5",
			lite:  `SELECT * FROM "t" LIMIT -1 OFFSET 5`,
		},
		{
			name: "prefix and schema",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("app.users as u").Prefix("p_").Where("u.id", 1)
			},
			pg:   `SELECT * FROM "app"."p_users" AS "p_u" WHERE "p_u"."id" = $1`,
			my:   "SELECT * FROM `app`.`p_users` AS `p_u` WHERE `p_u`.`id` = ?",
			lite: `SELECT * FROM "app"."p_users" AS "p_u" WHERE "p_u"."id" = ?`,
			args: []any{1},
		},
		{
			name: "identifier quote escaped",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").Select(`a"b` + "`c")
			},
			pg:   `SELECT "a""b` + "`c" + `" FROM "t"`,
			my:   "SELECT `a\"b``c` FROM `t`",
			lite: `SELECT "a""b` + "`c" + `" FROM "t"`,
		},
		{
			name: "when unless tap",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").
					When(true, func(q *db.Builder) { q.Where("a", 1) }).
					When(false, func(q *db.Builder) { q.Where("b", 2) }).
					Unless(false, func(q *db.Builder) { q.Where("c", 3) })
			},
			pg:   `SELECT * FROM "t" WHERE "a" = $1 AND "c" = $2`,
			my:   "SELECT * FROM `t` WHERE `a` = ? AND `c` = ?",
			lite: `SELECT * FROM "t" WHERE "a" = ? AND "c" = ?`,
			args: []any{1, 3},
		},
		{
			name: "expression value inlined",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").Where("a", "=", db.Raw("now() - interval ?", "1 day"))
			},
			pg:   `SELECT * FROM "t" WHERE "a" = now() - interval $1`,
			my:   "SELECT * FROM `t` WHERE `a` = now() - interval ?",
			lite: `SELECT * FROM "t" WHERE "a" = now() - interval ?`,
			args: []any{"1 day"},
		},
	}

	runGolden(t, tests)
}

func runGolden(t *testing.T, tests []golden) {
	t.Helper()
	dialects := []struct {
		d    db.Dialect
		want func(golden) string
	}{
		{db.Postgres, func(g golden) string { return g.pg }},
		{db.MySQL, func(g golden) string { return g.my }},
		{db.SQLite, func(g golden) string { return g.lite }},
		{db.MariaDB, func(g golden) string {
			if g.maria != "" {
				return g.maria
			}
			return g.my
		}},
	}
	for _, tc := range tests {
		for _, dl := range dialects {
			t.Run(tc.name+"/"+dl.d.String(), func(t *testing.T) {
				sql, args, err := tc.build(dl.d).ToSQL()
				require.NoError(t, err)
				assert.Equal(t, dl.want(tc), sql)
				if len(args) == 0 && len(tc.args) == 0 {
					return
				}
				assert.Equal(t, tc.args, args)
			})
		}
	}
}

func TestErrors(t *testing.T) {
	tests := []struct {
		name  string
		build *db.Builder
		want  error
	}{
		{"no table", db.NewBuilder(db.Postgres), db.ErrNoTable},
		{"bad operator", db.Use(db.Postgres).Table("t").Where("a", "; drop table t", 1), db.ErrInvalidOperator},
		{"bad direction", db.Use(db.Postgres).Table("t").OrderBy("a", "sideways"), db.ErrInvalidDirection},
		{"bad arg count", db.Use(db.Postgres).Table("t").Where("a"), db.ErrInvalidArgs},
		{"unknown dialect", db.Use(db.Dialect(99)).Table("t"), db.ErrUnknownDialect},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestCloneIndependent(t *testing.T) {
	base := db.Use(db.Postgres).Table("t").Where("a", 1)
	c := base.Clone().Where("b", 2).Limit(1)
	bs, _, _ := base.ToSQL()
	cs, _, _ := c.ToSQL()
	if bs != `SELECT * FROM "t" WHERE "a" = $1` {
		t.Errorf("base mutated: %s", bs)
	}
	if cs != `SELECT * FROM "t" WHERE "a" = $1 AND "b" = $2 LIMIT 1` {
		t.Errorf("clone: %s", cs)
	}
}

func TestToRawSQL(t *testing.T) {
	q := db.Use(db.Postgres).Table("t").Where("name", "o'brien").Where("n", ">", 3).Where("ok", true)
	got, err := q.ToRawSQL()
	require.NoError(t, err)
	want := `SELECT * FROM "t" WHERE "name" = 'o''brien' AND "n" > 3 AND "ok" = true`
	assert.Equal(t, want, got)

	m, _ := db.Use(db.MySQL).Table("t").Where("a", 1).Where("b", "x").ToRawSQL()
	if m != "SELECT * FROM `t` WHERE `a` = 1 AND `b` = 'x'" {
		t.Errorf("mysql raw: %s", m)
	}
}

func TestCloneKeepsUnionLimitAndLock(t *testing.T) {
	base := db.Use(db.Postgres).Table("a").Union(db.Use(db.Postgres).Table("b")).OrderBy("id").Limit(2).LockForUpdate()
	c := base.Clone().Limit(9)
	bs, _, _ := base.ToSQL()
	cs, _, _ := c.ToSQL()
	if want := `(SELECT * FROM "a" FOR UPDATE) UNION (SELECT * FROM "b") ORDER BY "id" ASC LIMIT 2`; bs != want {
		t.Errorf("base:\n got: %s\nwant: %s", bs, want)
	}
	if want := `(SELECT * FROM "a" FOR UPDATE) UNION (SELECT * FROM "b") ORDER BY "id" ASC LIMIT 9`; cs != want {
		t.Errorf("clone:\n got: %s\nwant: %s", cs, want)
	}
}

// TestBuilderWrappers pins the SQL of the small builder methods that the golden
// tables do not reach (raw OR, group by raw, take, tap, limit/offset clamping).
func TestBuilderWrappers(t *testing.T) {
	d := db.Postgres
	tb := func() *db.Builder { return db.Use(d).Table("t") }
	tests := []struct {
		name  string
		build *db.Builder
		sql   string
		args  []any
	}{
		{
			"or where raw",
			tb().Where("a", 1).OrWhereRaw("b = ?", 2),
			`SELECT * FROM "t" WHERE "a" = $1 OR b = $2`,
			[]any{1, 2},
		},
		{
			"group by raw, or having, having raw",
			tb().GroupByRaw("date(x)").Having("n", ">", 1).OrHaving("m", 2).HavingRaw("count(*) > ?", 3),
			`SELECT * FROM "t" GROUP BY date(x) HAVING "n" > $1 OR "m" = $2 AND count(*) > $3`,
			[]any{1, 2, 3},
		},
		{"take, and a page number below 1", tb().Take(3).ForPage(0, 5), `SELECT * FROM "t" LIMIT 5 OFFSET 0`, nil},
		{"tap", tb().Tap(func(q *db.Builder) { q.Where("a", 1) }), `SELECT * FROM "t" WHERE "a" = $1`, []any{1}},
		{
			"negative limit ignored, negative offset clamped",
			tb().Limit(-1).Offset(-5),
			`SELECT * FROM "t" OFFSET 0`,
			nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := tc.build.ToSQL()
			require.NoError(t, err)
			assert.Equal(t, tc.sql, sql)
			if len(args)+len(tc.args) > 0 && !reflect.DeepEqual(args, tc.args) {
				t.Errorf("args got %v want %v", args, tc.args)
			}
		})
	}
}

func TestErrAccessorAndRawSQLLiterals(t *testing.T) {
	b := db.Use(db.Postgres).Table("t").Where("a", "bogus", 1)
	if b.Err() == nil {
		t.Fatal("Err should expose the recorded error")
	}
	if _, err := b.ToRawSQL(); err == nil {
		t.Error("ToRawSQL should surface the error")
	}
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := db.Use(db.SQLite).Table("t").
		Where("a", nil).Where("b", []byte("x'y")).Where("c", at).Where("d", 1.5).Where("e", false).
		ToRawSQL()
	require.NoError(t, err)
	want := `SELECT * FROM "t" WHERE "a" IS NULL AND "b" = 'x''y' AND "c" = '2024-01-02 03:04:05' AND "d" = 1.5 AND "e" = false`
	assert.Equal(t, want, got)
}

func TestToRawSQLKeepsPostgresJSONQuestionMark(t *testing.T) {
	got, err := db.Use(db.Postgres).Table("t").WhereJsonContainsKey("o->k").Where("a", 1).ToRawSQL()
	require.NoError(t, err)
	want := `SELECT * FROM "t" WHERE COALESCE(("o")::JSONB ? 'k', FALSE) AND "a" = 1`
	assert.Equal(t, want, got)
}

func TestMustSQLAndMustRawSQL(t *testing.T) {
	q := func() *db.Builder {
		return db.Use(db.Postgres).Table("users").Where("name", "o'brien").Where("age", ">", 30)
	}

	sql, args := q().MustSQL()
	wantSQL, wantArgs, err := q().ToSQL()
	if err != nil || sql != wantSQL || !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("MustSQL differs from ToSQL: %s %v vs %s %v (%v)", sql, args, wantSQL, wantArgs, err)
	}
	raw := q().MustRawSQL()
	if want := `SELECT * FROM "users" WHERE "name" = 'o''brien' AND "age" > 30`; raw != want {
		t.Errorf("MustRawSQL = %s, want %s", raw, want)
	}

	// a failure panics with the original error, still matchable with errors.Is
	bad := map[string]struct {
		call func()
		want error
	}{
		"MustSQL, invalid operator": {
			func() { db.Use(db.Postgres).Table("t").Where("a", "bogus", 1).MustSQL() },
			db.ErrInvalidOperator,
		},
		"MustSQL, no table":        {func() { db.Use(db.Postgres).NewQuery().MustSQL() }, db.ErrNoTable},
		"MustSQL, unknown dialect": {func() { db.Use(db.Dialect(99)).Table("t").MustSQL() }, db.ErrUnknownDialect},
		"MustRawSQL, invalid operator": {
			func() { db.Use(db.Postgres).Table("t").Where("a", "bogus", 1).MustRawSQL() },
			db.ErrInvalidOperator,
		},
		"MustRawSQL, no table": {func() { db.Use(db.Postgres).NewQuery().MustRawSQL() }, db.ErrNoTable},
	}
	for name, tc := range bad {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				err, ok := r.(error)
				if !ok {
					t.Fatalf("panic value is %T (%v), want an error", r, r)
				}
				assert.ErrorIs(t, err, tc.want)
			}()
			tc.call()
			t.Error("expected a panic")
		})
	}
}
