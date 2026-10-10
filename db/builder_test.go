package db_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

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
			pg:    `select * from "users"`,
			my:    "select * from `users`",
			lite:  `select * from "users"`,
		},
		{
			name: "columns alias distinct qualified",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("users as u").Distinct().Select("u.id", "u.name as n", "u.*")
			},
			pg:   `select distinct "u"."id", "u"."name" as "n", "u".* from "users" as "u"`,
			my:   "select distinct `u`.`id`, `u`.`name` as `n`, `u`.* from `users` as `u`",
			lite: `select distinct "u"."id", "u"."name" as "n", "u".* from "users" as "u"`,
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
			pg:   `select * from "users" where "votes" > $1 or "name" = $2 and "deleted_at" is null and "role" is not null`,
			my:   "select * from `users` where `votes` > ? or `name` = ? and `deleted_at` is null and `role` is not null",
			lite: `select * from "users" where "votes" > ? or "name" = ? and "deleted_at" is null and "role" is not null`,
			args: []any{100, "john"},
		},
		{
			name: "raw keeps placeholders in order",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").
					SelectRaw("count(*) filter (where x > ?) as c", 5).
					WhereRaw("a = ? and b = '?' and c ?? 'k'", 1).
					Where("z", 9)
			},
			pg:   `select count(*) filter (where x > $1) as c from "t" where a = $2 and b = '?' and c ? 'k' and "z" = $3`,
			my:   "select count(*) filter (where x > ?) as c from `t` where a = ? and b = '?' and c ? 'k' and `z` = ?",
			lite: `select count(*) filter (where x > ?) as c from "t" where a = ? and b = '?' and c ? 'k' and "z" = ?`,
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
			pg:   `select "user_id", sum(total) as s from "orders" group by "user_id" having "s" > $1 or count(*) > $2 order by "user_id" asc, "s" desc, random() limit 10 offset 20`,
			my:   "select `user_id`, sum(total) as s from `orders` group by `user_id` having `s` > ? or count(*) > ? order by `user_id` asc, `s` desc, random() limit 10 offset 20",
			lite: `select "user_id", sum(total) as s from "orders" group by "user_id" having "s" > ? or count(*) > ? order by "user_id" asc, "s" desc, random() limit 10 offset 20`,
			args: []any{10, 3},
		},
		{
			name:  "for page",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").ForPage(3, 15) },
			pg:    `select * from "t" limit 15 offset 30`,
			my:    "select * from `t` limit 15 offset 30",
			lite:  `select * from "t" limit 15 offset 30`,
		},
		{
			name:  "offset without limit",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").Skip(5) },
			pg:    `select * from "t" offset 5`,
			my:    "select * from `t` limit 18446744073709551615 offset 5",
			lite:  `select * from "t" limit -1 offset 5`,
		},
		{
			name: "prefix and schema",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("app.users as u").Prefix("p_").Where("u.id", 1)
			},
			pg:   `select * from "app"."p_users" as "p_u" where "p_u"."id" = $1`,
			my:   "select * from `app`.`p_users` as `p_u` where `p_u`.`id` = ?",
			lite: `select * from "app"."p_users" as "p_u" where "p_u"."id" = ?`,
			args: []any{1},
		},
		{
			name: "identifier quote escaped",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").Select(`a"b` + "`c")
			},
			pg:   `select "a""b` + "`c" + `" from "t"`,
			my:   "select `a\"b``c` from `t`",
			lite: `select "a""b` + "`c" + `" from "t"`,
		},
		{
			name: "when unless tap",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").
					When(true, func(q *db.Builder) { q.Where("a", 1) }).
					When(false, func(q *db.Builder) { q.Where("b", 2) }).
					Unless(false, func(q *db.Builder) { q.Where("c", 3) })
			},
			pg:   `select * from "t" where "a" = $1 and "c" = $2`,
			my:   "select * from `t` where `a` = ? and `c` = ?",
			lite: `select * from "t" where "a" = ? and "c" = ?`,
			args: []any{1, 3},
		},
		{
			name: "expression value inlined",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").Where("a", "=", db.Raw("now() - interval ?", "1 day"))
			},
			pg:   `select * from "t" where "a" = now() - interval $1`,
			my:   "select * from `t` where `a` = now() - interval ?",
			lite: `select * from "t" where "a" = now() - interval ?`,
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
				if err != nil {
					t.Fatal(err)
				}
				if want := dl.want(tc); sql != want {
					t.Errorf("sql\n got: %s\nwant: %s", sql, want)
				}
				if len(args) == 0 && len(tc.args) == 0 {
					return
				}
				if !reflect.DeepEqual(args, tc.args) {
					t.Errorf("args got %v want %v", args, tc.args)
				}
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
			if _, _, err := tc.build.ToSQL(); !errors.Is(err, tc.want) {
				t.Errorf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestCloneIndependent(t *testing.T) {
	base := db.Use(db.Postgres).Table("t").Where("a", 1)
	c := base.Clone().Where("b", 2).Limit(1)
	bs, _, _ := base.ToSQL()
	cs, _, _ := c.ToSQL()
	if bs != `select * from "t" where "a" = $1` {
		t.Errorf("base mutated: %s", bs)
	}
	if cs != `select * from "t" where "a" = $1 and "b" = $2 limit 1` {
		t.Errorf("clone: %s", cs)
	}
}

func TestToRawSQL(t *testing.T) {
	q := db.Use(db.Postgres).Table("t").Where("name", "o'brien").Where("n", ">", 3).Where("ok", true)
	got, err := q.ToRawSQL()
	if err != nil {
		t.Fatal(err)
	}
	want := `select * from "t" where "name" = 'o''brien' and "n" > 3 and "ok" = true`
	if got != want {
		t.Errorf("got %s want %s", got, want)
	}

	m, _ := db.Use(db.MySQL).Table("t").Where("a", 1).Where("b", "x").ToRawSQL()
	if m != "select * from `t` where `a` = 1 and `b` = 'x'" {
		t.Errorf("mysql raw: %s", m)
	}
}

func TestCloneKeepsUnionLimitAndLock(t *testing.T) {
	base := db.Use(db.Postgres).Table("a").Union(db.Use(db.Postgres).Table("b")).OrderBy("id").Limit(2).LockForUpdate()
	c := base.Clone().Limit(9)
	bs, _, _ := base.ToSQL()
	cs, _, _ := c.ToSQL()
	if want := `(select * from "a" for update) union (select * from "b") order by "id" asc limit 2`; bs != want {
		t.Errorf("base:\n got: %s\nwant: %s", bs, want)
	}
	if want := `(select * from "a" for update) union (select * from "b") order by "id" asc limit 9`; cs != want {
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
		{"or where raw", tb().Where("a", 1).OrWhereRaw("b = ?", 2), `select * from "t" where "a" = $1 or b = $2`, []any{1, 2}},
		{"group by raw, or having, having raw", tb().GroupByRaw("date(x)").Having("n", ">", 1).OrHaving("m", 2).HavingRaw("count(*) > ?", 3),
			`select * from "t" group by date(x) having "n" > $1 or "m" = $2 and count(*) > $3`, []any{1, 2, 3}},
		{"take, and a page number below 1", tb().Take(3).ForPage(0, 5), `select * from "t" limit 5 offset 0`, nil},
		{"tap", tb().Tap(func(q *db.Builder) { q.Where("a", 1) }), `select * from "t" where "a" = $1`, []any{1}},
		{"negative limit ignored, negative offset clamped", tb().Limit(-1).Offset(-5), `select * from "t" offset 0`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sql, args, err := tc.build.ToSQL()
			if err != nil {
				t.Fatal(err)
			}
			if sql != tc.sql {
				t.Errorf("sql\n got: %s\nwant: %s", sql, tc.sql)
			}
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
	if err != nil {
		t.Fatal(err)
	}
	want := `select * from "t" where "a" is null and "b" = 'x''y' and "c" = '2024-01-02 03:04:05' and "d" = 1.5 and "e" = false`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestToRawSQLKeepsPostgresJSONQuestionMark(t *testing.T) {
	got, err := db.Use(db.Postgres).Table("t").WhereJsonContainsKey("o->k").Where("a", 1).ToRawSQL()
	if err != nil {
		t.Fatal(err)
	}
	want := `select * from "t" where coalesce(("o")::jsonb ? 'k', false) and "a" = 1`
	if got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
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
	if want := `select * from "users" where "name" = 'o''brien' and "age" > 30`; raw != want {
		t.Errorf("MustRawSQL = %s, want %s", raw, want)
	}

	// a failure panics with the original error, still matchable with errors.Is
	bad := map[string]struct {
		call func()
		want error
	}{
		"MustSQL, invalid operator":    {func() { db.Use(db.Postgres).Table("t").Where("a", "bogus", 1).MustSQL() }, db.ErrInvalidOperator},
		"MustSQL, no table":            {func() { db.Use(db.Postgres).NewQuery().MustSQL() }, db.ErrNoTable},
		"MustSQL, unknown dialect":     {func() { db.Use(db.Dialect(99)).Table("t").MustSQL() }, db.ErrUnknownDialect},
		"MustRawSQL, invalid operator": {func() { db.Use(db.Postgres).Table("t").Where("a", "bogus", 1).MustRawSQL() }, db.ErrInvalidOperator},
		"MustRawSQL, no table":         {func() { db.Use(db.Postgres).NewQuery().MustRawSQL() }, db.ErrNoTable},
	}
	for name, tc := range bad {
		t.Run(name, func(t *testing.T) {
			defer func() {
				r := recover()
				err, ok := r.(error)
				if !ok {
					t.Fatalf("panic value is %T (%v), want an error", r, r)
				}
				if !errors.Is(err, tc.want) {
					t.Errorf("recovered %v, want it to wrap %v", err, tc.want)
				}
			}()
			tc.call()
			t.Error("expected a panic")
		})
	}
}
