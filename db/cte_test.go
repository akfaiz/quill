package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func TestCTEGolden(t *testing.T) {
	active := func(d db.Dialect) *db.Builder { return db.Use(d).Table("users").Where("active", true) }
	tests := []golden{
		{
			name:  "single",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("au").With("au", active(d)).Where("id", ">", 5) },
			pg:    `WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1) SELECT * FROM "au" WHERE "id" > $2`,
			my:    "WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) SELECT * FROM `au` WHERE `id` > ?",
			lite:  `WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT * FROM "au" WHERE "id" > ?`,
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
			pg:   `WITH "a" ("uid", "uname") AS (SELECT "id", "name" FROM "users"), "b" AS (SELECT * FROM "posts" WHERE "uid" = $1) SELECT * FROM "a" INNER JOIN "b" ON "b"."uid" = "a"."uid"`,
			my:   "WITH `a` (`uid`, `uname`) AS (SELECT `id`, `name` FROM `users`), `b` AS (SELECT * FROM `posts` WHERE `uid` = ?) SELECT * FROM `a` INNER JOIN `b` ON `b`.`uid` = `a`.`uid`",
			lite: `WITH "a" ("uid", "uname") AS (SELECT "id", "name" FROM "users"), "b" AS (SELECT * FROM "posts" WHERE "uid" = ?) SELECT * FROM "a" INNER JOIN "b" ON "b"."uid" = "a"."uid"`,
			args: []any{1},
		},
		{
			name: "recursive members are not wrapped",
			build: func(d db.Dialect) *db.Builder {
				anchor := db.Use(d).NewQuery().SelectRaw("1 as n").FromRaw("(SELECT 1) s")
				step := db.Use(d).Table("seq").SelectRaw("n + 1").Where("n", "<", 5)
				return db.Use(d).Table("seq").WithRecursive("seq", anchor.UnionAll(step), "n")
			},
			pg:   `WITH RECURSIVE "seq" ("n") AS (SELECT 1 as n FROM (SELECT 1) s UNION ALL SELECT n + 1 FROM "seq" WHERE "n" < $1) SELECT * FROM "seq"`,
			my:   "WITH RECURSIVE `seq` (`n`) AS (SELECT 1 as n FROM (SELECT 1) s UNION ALL SELECT n + 1 FROM `seq` WHERE `n` < ?) SELECT * FROM `seq`",
			lite: `WITH RECURSIVE "seq" ("n") AS (SELECT 1 as n FROM (SELECT 1) s UNION ALL SELECT n + 1 FROM "seq" WHERE "n" < ?) SELECT * FROM "seq"`,
			args: []any{5},
		},
		{
			name: "recursive keyword once for mixed list",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("r").With("a", db.Use(d).Table("x")).WithRecursive("r", db.Use(d).Table("a"))
			},
			pg:   `WITH RECURSIVE "a" AS (SELECT * FROM "x"), "r" AS (SELECT * FROM "a") SELECT * FROM "r"`,
			my:   "WITH RECURSIVE `a` AS (SELECT * FROM `x`), `r` AS (SELECT * FROM `a`) SELECT * FROM `r`",
			lite: `WITH RECURSIVE "a" AS (SELECT * FROM "x"), "r" AS (SELECT * FROM "a") SELECT * FROM "r"`,
		},
		{
			name: "with prefixes an outer union",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("au").With("au", active(d)).Union(db.Use(d).Table("au").Where("id", 1))
			},
			pg:   `WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1) (SELECT * FROM "au") UNION (SELECT * FROM "au" WHERE "id" = $2)`,
			my:   "WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) (SELECT * FROM `au`) UNION (SELECT * FROM `au` WHERE `id` = ?)",
			lite: `WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT * FROM (SELECT * FROM "au") UNION SELECT * FROM (SELECT * FROM "au" WHERE "id" = ?)`,
			args: []any{true, 1},
		},
		{
			name: "with inside a subquery keeps placeholder order",
			build: func(d db.Dialect) *db.Builder {
				sub := db.Use(d).Table("au").Select("id").With("au", active(d))
				return db.Use(d).Table("posts").Where("draft", false).WhereIn("uid", sub).Where("id", ">", 9)
			},
			pg:   `SELECT * FROM "posts" WHERE "draft" = $1 AND "uid" IN (WITH "au" AS (SELECT * FROM "users" WHERE "active" = $2) SELECT "id" FROM "au") AND "id" > $3`,
			my:   "SELECT * FROM `posts` WHERE `draft` = ? AND `uid` IN (WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) SELECT `id` FROM `au`) AND `id` > ?",
			lite: `SELECT * FROM "posts" WHERE "draft" = ? AND "uid" IN (WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT "id" FROM "au") AND "id" > ?`,
			args: []any{false, true, 9},
		},
		{
			name: "raw body",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("v").WithRaw("v", db.Raw("SELECT ? UNION ALL SELECT ?", 1, 2), "n")
			},
			pg:   `WITH "v" ("n") AS (SELECT $1 UNION ALL SELECT $2) SELECT * FROM "v"`,
			my:   "WITH `v` (`n`) AS (SELECT ? UNION ALL SELECT ?) SELECT * FROM `v`",
			lite: `WITH "v" ("n") AS (SELECT ? UNION ALL SELECT ?) SELECT * FROM "v"`,
			args: []any{1, 2},
		},
		{
			name: "prefix applies to the name",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d, db.WithPrefix("app_")).
					Table("au").
					With("au", db.Use(d, db.WithPrefix("app_")).Table("users"))
			},
			pg:   `WITH "app_au" AS (SELECT * FROM "app_users") SELECT * FROM "app_au"`,
			my:   "WITH `app_au` AS (SELECT * FROM `app_users`) SELECT * FROM `app_au`",
			lite: `WITH "app_au" AS (SELECT * FROM "app_users") SELECT * FROM "app_au"`,
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
			pg:    want{sql: `WITH "x" AS MATERIALIZED (SELECT * FROM "t") SELECT * FROM "x"`},
			my:    unsup, lite: unsup,
		},
		{
			name: "not materialized",
			build: selectOf(
				func(d db.Dialect) *db.Builder { return q(d).WithNotMaterialized("x", db.Use(d).Table("t")) },
			),
			pg:   want{sql: `WITH "x" AS NOT MATERIALIZED (SELECT * FROM "t") SELECT * FROM "x"`},
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
			pg:   `INSERT INTO "archive" ("name") WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1) SELECT "name" FROM "au"`,
			my:   "INSERT INTO `archive` (`name`) WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) SELECT `name` FROM `au`",
			lite: `INSERT INTO "archive" ("name") WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT "name" FROM "au"`,
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
		{"duplicate name", x().With("c", x()).WithRaw("c", db.Raw("SELECT 1")), db.ErrInvalidArgs},
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
	if want := `WITH "c" AS (SELECT * FROM "users") SELECT * FROM "c"`; got != want {
		t.Errorf("base\n got: %s\nwant: %s", got, want)
	}
	got, _, err = clone.ToSQL()
	require.NoError(t, err)
	if want := `WITH "c" AS (SELECT * FROM "users"), "d" AS (SELECT * FROM "posts") SELECT * FROM "c"`; got != want {
		t.Errorf("clone\n got: %s\nwant: %s", got, want)
	}
}
