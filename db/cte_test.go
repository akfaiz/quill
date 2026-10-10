package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/assert"

	"github.com/akfaiz/quill/db"
)

func TestCTEGolden(t *testing.T) {
	active := func(d db.Dialect) *db.Builder { return db.Use(d).Table("users").Where("active", true) }
	tests := []golden{
		{
			name:  "single",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("au").With("au", active(d)).Where("id", ">", 5) },
			pg:    `with "au" as (select * from "users" where "active" = $1) select * from "au" where "id" > $2`,
			my:    "with `au` as (select * from `users` where `active` = ?) select * from `au` where `id` > ?",
			lite:  `with "au" as (select * from "users" where "active" = ?) select * from "au" where "id" > ?`,
			args:  []any{true, 5},
		},
		{
			name: "multiple with columns",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("a").
					With("a", db.Use(d).Table("users").Select("id", "name"), "uid", "uname").
					With("b", db.Use(d).Table("posts").Where("uid", 1)).
					Join("b", "b.uid", "a.uid")
			},
			pg:   `with "a" ("uid", "uname") as (select "id", "name" from "users"), "b" as (select * from "posts" where "uid" = $1) select * from "a" inner join "b" on "b"."uid" = "a"."uid"`,
			my:   "with `a` (`uid`, `uname`) as (select `id`, `name` from `users`), `b` as (select * from `posts` where `uid` = ?) select * from `a` inner join `b` on `b`.`uid` = `a`.`uid`",
			lite: `with "a" ("uid", "uname") as (select "id", "name" from "users"), "b" as (select * from "posts" where "uid" = ?) select * from "a" inner join "b" on "b"."uid" = "a"."uid"`,
			args: []any{1},
		},
		{
			name: "recursive members are not wrapped",
			build: func(d db.Dialect) *db.Builder {
				anchor := db.Use(d).NewQuery().SelectRaw("1 as n").FromRaw("(select 1) s")
				step := db.Use(d).Table("seq").SelectRaw("n + 1").Where("n", "<", 5)
				return db.Use(d).Table("seq").WithRecursive("seq", anchor.UnionAll(step), "n")
			},
			pg:   `with recursive "seq" ("n") as (select 1 as n from (select 1) s union all select n + 1 from "seq" where "n" < $1) select * from "seq"`,
			my:   "with recursive `seq` (`n`) as (select 1 as n from (select 1) s union all select n + 1 from `seq` where `n` < ?) select * from `seq`",
			lite: `with recursive "seq" ("n") as (select 1 as n from (select 1) s union all select n + 1 from "seq" where "n" < ?) select * from "seq"`,
			args: []any{5},
		},
		{
			name: "recursive keyword once for mixed list",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("r").With("a", db.Use(d).Table("x")).WithRecursive("r", db.Use(d).Table("a"))
			},
			pg:   `with recursive "a" as (select * from "x"), "r" as (select * from "a") select * from "r"`,
			my:   "with recursive `a` as (select * from `x`), `r` as (select * from `a`) select * from `r`",
			lite: `with recursive "a" as (select * from "x"), "r" as (select * from "a") select * from "r"`,
		},
		{
			name: "with prefixes an outer union",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("au").With("au", active(d)).Union(db.Use(d).Table("au").Where("id", 1))
			},
			pg:   `with "au" as (select * from "users" where "active" = $1) (select * from "au") union (select * from "au" where "id" = $2)`,
			my:   "with `au` as (select * from `users` where `active` = ?) (select * from `au`) union (select * from `au` where `id` = ?)",
			lite: `with "au" as (select * from "users" where "active" = ?) select * from (select * from "au") union select * from (select * from "au" where "id" = ?)`,
			args: []any{true, 1},
		},
		{
			name: "with inside a subquery keeps placeholder order",
			build: func(d db.Dialect) *db.Builder {
				sub := db.Use(d).Table("au").Select("id").With("au", active(d))
				return db.Use(d).Table("posts").Where("draft", false).WhereIn("uid", sub).Where("id", ">", 9)
			},
			pg:   `select * from "posts" where "draft" = $1 and "uid" in (with "au" as (select * from "users" where "active" = $2) select "id" from "au") and "id" > $3`,
			my:   "select * from `posts` where `draft` = ? and `uid` in (with `au` as (select * from `users` where `active` = ?) select `id` from `au`) and `id` > ?",
			lite: `select * from "posts" where "draft" = ? and "uid" in (with "au" as (select * from "users" where "active" = ?) select "id" from "au") and "id" > ?`,
			args: []any{false, true, 9},
		},
		{
			name: "raw body",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("v").WithRaw("v", db.Raw("select ? union all select ?", 1, 2), "n")
			},
			pg:   `with "v" ("n") as (select $1 union all select $2) select * from "v"`,
			my:   "with `v` (`n`) as (select ? union all select ?) select * from `v`",
			lite: `with "v" ("n") as (select ? union all select ?) select * from "v"`,
			args: []any{1, 2},
		},
		{
			name: "prefix applies to the name",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d, db.WithPrefix("app_")).
					Table("au").
					With("au", db.Use(d, db.WithPrefix("app_")).Table("users"))
			},
			pg:   `with "app_au" as (select * from "app_users") select * from "app_au"`,
			my:   "with `app_au` as (select * from `app_users`) select * from `app_au`",
			lite: `with "app_au" as (select * from "app_users") select * from "app_au"`,
		},
	}
	runGolden(t, tests)
}

func TestCTEMaterialized(t *testing.T) {
	q := func(d db.Dialect) *db.Builder { return db.Use(d).Table("x") }
	unsup := want{err: db.ErrUnsupported}
	runPer(t, []perCase{
		{
			name:  "materialized",
			build: selectOf(func(d db.Dialect) *db.Builder { return q(d).WithMaterialized("x", db.Use(d).Table("t")) }),
			pg:    want{sql: `with "x" as materialized (select * from "t") select * from "x"`},
			my:    unsup, lite: unsup,
		},
		{
			name: "not materialized",
			build: selectOf(
				func(d db.Dialect) *db.Builder { return q(d).WithNotMaterialized("x", db.Use(d).Table("t")) },
			),
			pg:   want{sql: `with "x" as not materialized (select * from "t") select * from "x"`},
			my:   unsup,
			lite: unsup,
		},
	})
}

func TestCTEWrites(t *testing.T) {
	runStmts(t, []stmtCase{
		{
			name: "insert using a select with a cte",
			build: func(d db.Dialect) (string, []any, error) {
				sub := db.Use(d).Table("au").Select("name").With("au", db.Use(d).Table("users").Where("active", true))
				return db.Use(d).Table("archive").InsertUsingSQL([]string{"name"}, sub)
			},
			pg:   `insert into "archive" ("name") with "au" as (select * from "users" where "active" = $1) select "name" from "au"`,
			my:   "insert into `archive` (`name`) with `au` as (select * from `users` where `active` = ?) select `name` from `au`",
			lite: `insert into "archive" ("name") with "au" as (select * from "users" where "active" = ?) select "name" from "au"`,
			args: []any{true},
		},
		{
			name: "with on update is refused",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).
					Table("users").
					With("x", db.Use(d).Table("y")).
					Where("id", 1).
					UpdateSQL(map[string]any{"a": 1})
			},
			pg: unsupported, my: unsupported, lite: unsupported,
		},
		{
			name: "with on delete is refused",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").With("x", db.Use(d).Table("y")).Where("id", 1).DeleteSQL()
			},
			pg: unsupported, my: unsupported, lite: unsupported,
		},
		{
			name: "with on insert is refused",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").With("x", db.Use(d).Table("y")).InsertSQL(map[string]any{"a": 1})
			},
			pg: unsupported, my: unsupported, lite: unsupported,
		},
	})
}

func TestCTEErrors(t *testing.T) {
	d := db.Postgres
	x := func() *db.Builder { return db.Use(d).Table("x") }
	tests := []struct {
		name  string
		build *db.Builder
		want  error
	}{
		{"empty name", x().With("", x()), db.ErrInvalidArgs},
		{"qualified name", x().With("s.x", x()), db.ErrInvalidArgs},
		{"aliased name", x().With("x as y", x()), db.ErrInvalidArgs},
		{"nil query", x().With("c", nil), db.ErrInvalidArgs},
		{"empty column", x().With("c", x(), "a", ""), db.ErrInvalidArgs},
		{"duplicate name", x().With("c", x()).WithRaw("c", db.Raw("select 1")), db.ErrInvalidArgs},
		{"error inside cte", x().With("c", db.NewBuilder(d)), db.ErrNoTable},
		{"error inside recursive member", x().WithRecursive("c", x().UnionAll(db.NewBuilder(d))), db.ErrNoTable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestCTESnapshotAndClone(t *testing.T) {
	sub := db.Use(db.Postgres).Table("users")
	base := db.Use(db.Postgres).Table("c").With("c", sub)
	sub.Where("late", 1)
	clone := base.Clone().With("d", db.Use(db.Postgres).Table("posts"))

	got, _, err := base.ToSQL()
	require.NoError(t, err)
	if want := `with "c" as (select * from "users") select * from "c"`; got != want {
		t.Errorf("base\n got: %s\nwant: %s", got, want)
	}
	got, _, err = clone.ToSQL()
	require.NoError(t, err)
	if want := `with "c" as (select * from "users"), "d" as (select * from "posts") select * from "c"`; got != want {
		t.Errorf("clone\n got: %s\nwant: %s", got, want)
	}
}
