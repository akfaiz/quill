package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func TestCTEGolden(t *testing.T) {
	active := func(d db.Dialect) *db.SelectBuilder { return db.Use(d).Select().From("users").Where("active", true) }
	tests := []golden{
		{
			name: "single",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("au").With("au", active(d)).Where("id", ">", 5)
			},
			pg:   `WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1) SELECT * FROM "au" WHERE "id" > $2`,
			my:   "WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) SELECT * FROM `au` WHERE `id` > ?",
			lite: `WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT * FROM "au" WHERE "id" > ?`,
			args: []any{true, 5},
		},
		{
			name: "multiple with columns",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("a").
					With("a", db.Use(d).Select().From("users").Select("id", "name"), db.CTEColumns("uid", "uname")).
					With("b", db.Use(d).Select().From("posts").Where("uid", 1)).
					Join("b", "b.uid", "a.uid")
			},
			pg:   `WITH "a" ("uid", "uname") AS (SELECT "id", "name" FROM "users"), "b" AS (SELECT * FROM "posts" WHERE "uid" = $1) SELECT * FROM "a" INNER JOIN "b" ON "b"."uid" = "a"."uid"`,
			my:   "WITH `a` (`uid`, `uname`) AS (SELECT `id`, `name` FROM `users`), `b` AS (SELECT * FROM `posts` WHERE `uid` = ?) SELECT * FROM `a` INNER JOIN `b` ON `b`.`uid` = `a`.`uid`",
			lite: `WITH "a" ("uid", "uname") AS (SELECT "id", "name" FROM "users"), "b" AS (SELECT * FROM "posts" WHERE "uid" = ?) SELECT * FROM "a" INNER JOIN "b" ON "b"."uid" = "a"."uid"`,
			args: []any{1},
		},
		{
			name: "recursive members are not wrapped",
			build: func(d db.Dialect) *db.SelectBuilder {
				anchor := db.Use(d).Select().SelectRaw("1 as n").FromRaw("(SELECT 1) s")
				step := db.Use(d).Select().From("seq").SelectRaw("n + 1").Where("n", "<", 5)
				return db.Use(d).Select().From("seq").WithRecursive("seq", anchor, step, db.CTEColumns("n"))
			},
			pg:   `WITH RECURSIVE "seq" ("n") AS (SELECT 1 as n FROM (SELECT 1) s UNION ALL SELECT n + 1 FROM "seq" WHERE "n" < $1) SELECT * FROM "seq"`,
			my:   "WITH RECURSIVE `seq` (`n`) AS (SELECT 1 as n FROM (SELECT 1) s UNION ALL SELECT n + 1 FROM `seq` WHERE `n` < ?) SELECT * FROM `seq`",
			lite: `WITH RECURSIVE "seq" ("n") AS (SELECT 1 as n FROM (SELECT 1) s UNION ALL SELECT n + 1 FROM "seq" WHERE "n" < ?) SELECT * FROM "seq"`,
			args: []any{5},
		},
		{
			name: "recursive keyword once for mixed list",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).
					Select().
					From("r").
					With("a", db.Use(d).Select().From("x")).
					WithRecursive("r", db.Use(d).Select().From("a"), db.Use(d).Select().From("r"))
			},
			pg:   `WITH RECURSIVE "a" AS (SELECT * FROM "x"), "r" AS (SELECT * FROM "a" UNION ALL SELECT * FROM "r") SELECT * FROM "r"`,
			my:   "WITH RECURSIVE `a` AS (SELECT * FROM `x`), `r` AS (SELECT * FROM `a` UNION ALL SELECT * FROM `r`) SELECT * FROM `r`",
			lite: `WITH RECURSIVE "a" AS (SELECT * FROM "x"), "r" AS (SELECT * FROM "a" UNION ALL SELECT * FROM "r") SELECT * FROM "r"`,
		},
		{
			name: "recursive union distinct",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("r").
					WithRecursive("r", db.Use(d).Select().From("a"), db.Use(d).Select().From("r"), db.CTEUnionDistinct())
			},
			pg:   `WITH RECURSIVE "r" AS (SELECT * FROM "a" UNION SELECT * FROM "r") SELECT * FROM "r"`,
			my:   "WITH RECURSIVE `r` AS (SELECT * FROM `a` UNION SELECT * FROM `r`) SELECT * FROM `r`",
			lite: `WITH RECURSIVE "r" AS (SELECT * FROM "a" UNION SELECT * FROM "r") SELECT * FROM "r"`,
		},
		{
			name: "raw body takes every option",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).Select().From("v").WithRaw("v", db.Raw("SELECT 1"), db.CTEColumns("n"))
			},
			pg:   `WITH "v" ("n") AS (SELECT 1) SELECT * FROM "v"`,
			my:   "WITH `v` (`n`) AS (SELECT 1) SELECT * FROM `v`",
			lite: `WITH "v" ("n") AS (SELECT 1) SELECT * FROM "v"`,
		},
		{
			name: "with prefixes an outer union",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).
					Select().
					From("au").
					With("au", active(d)).
					Union(db.Use(d).Select().From("au").Where("id", 1))
			},
			pg:   `WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1) (SELECT * FROM "au") UNION (SELECT * FROM "au" WHERE "id" = $2)`,
			my:   "WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) (SELECT * FROM `au`) UNION (SELECT * FROM `au` WHERE `id` = ?)",
			lite: `WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT * FROM (SELECT * FROM "au") UNION SELECT * FROM (SELECT * FROM "au" WHERE "id" = ?)`,
			args: []any{true, 1},
		},
		{
			name: "with inside a subquery keeps placeholder order",
			build: func(d db.Dialect) *db.SelectBuilder {
				sub := db.Use(d).Select().From("au").Select("id").With("au", active(d))
				return db.Use(d).Select().From("posts").Where("draft", false).WhereIn("uid", sub).Where("id", ">", 9)
			},
			pg:   `SELECT * FROM "posts" WHERE "draft" = $1 AND "uid" IN (WITH "au" AS (SELECT * FROM "users" WHERE "active" = $2) SELECT "id" FROM "au") AND "id" > $3`,
			my:   "SELECT * FROM `posts` WHERE `draft` = ? AND `uid` IN (WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) SELECT `id` FROM `au`) AND `id` > ?",
			lite: `SELECT * FROM "posts" WHERE "draft" = ? AND "uid" IN (WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT "id" FROM "au") AND "id" > ?`,
			args: []any{false, true, 9},
		},
		{
			name: "raw body",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d).
					Select().
					From("v").
					WithRaw("v", db.Raw("SELECT ? UNION ALL SELECT ?", 1, 2), db.CTEColumns("n"))
			},
			pg:   `WITH "v" ("n") AS (SELECT $1 UNION ALL SELECT $2) SELECT * FROM "v"`,
			my:   "WITH `v` (`n`) AS (SELECT ? UNION ALL SELECT ?) SELECT * FROM `v`",
			lite: `WITH "v" ("n") AS (SELECT ? UNION ALL SELECT ?) SELECT * FROM "v"`,
			args: []any{1, 2},
		},
		{
			name: "prefix applies to the name",
			build: func(d db.Dialect) *db.SelectBuilder {
				return db.Use(d, db.WithPrefix("app_")).
					Select().From("au").
					With("au", db.Use(d, db.WithPrefix("app_")).Select().From("users"))
			},
			pg:   `WITH "app_au" AS (SELECT * FROM "app_users") SELECT * FROM "app_au"`,
			my:   "WITH `app_au` AS (SELECT * FROM `app_users`) SELECT * FROM `app_au`",
			lite: `WITH "app_au" AS (SELECT * FROM "app_users") SELECT * FROM "app_au"`,
		},
	}
	runGolden(t, tests)
}

func TestCTEMaterialized(t *testing.T) {
	q := func(d db.Dialect) *db.SelectBuilder { return db.Use(d).Select().From("x") }
	body := func(d db.Dialect) *db.SelectBuilder { return db.Use(d).Select().From("t") }
	unsup := want{err: db.ErrUnsupported}
	runPer(t, []perCase{
		{
			name: "materialized",
			build: selectOf(
				func(d db.Dialect) *db.SelectBuilder { return q(d).With("x", body(d), db.CTEMaterialized()) },
			),
			pg:   want{sql: `WITH "x" AS MATERIALIZED (SELECT * FROM "t") SELECT * FROM "x"`},
			my:   unsup,
			lite: want{sql: `WITH "x" AS MATERIALIZED (SELECT * FROM "t") SELECT * FROM "x"`},
		},
		{
			name: "not materialized",
			build: selectOf(
				func(d db.Dialect) *db.SelectBuilder { return q(d).With("x", body(d), db.CTENotMaterialized()) },
			),
			pg:   want{sql: `WITH "x" AS NOT MATERIALIZED (SELECT * FROM "t") SELECT * FROM "x"`},
			my:   unsup,
			lite: want{sql: `WITH "x" AS NOT MATERIALIZED (SELECT * FROM "t") SELECT * FROM "x"`},
		},
		{
			name: "options combine: columns and materialized on a raw body",
			build: selectOf(func(d db.Dialect) *db.SelectBuilder {
				return q(d).WithRaw("x", db.Raw("SELECT 1"), db.CTEColumns("n"), db.CTEMaterialized())
			}),
			pg:   want{sql: `WITH "x" ("n") AS MATERIALIZED (SELECT 1) SELECT * FROM "x"`},
			my:   unsup,
			lite: want{sql: `WITH "x" ("n") AS MATERIALIZED (SELECT 1) SELECT * FROM "x"`},
		},
		{
			name: "options combine: recursive and not materialized",
			build: selectOf(func(d db.Dialect) *db.SelectBuilder {
				return q(d).WithRecursive("x", body(d), q(d), db.CTENotMaterialized())
			}),
			pg: want{
				sql: `WITH RECURSIVE "x" AS NOT MATERIALIZED (SELECT * FROM "t" UNION ALL SELECT * FROM "x") SELECT * FROM "x"`,
			},
			my: unsup,
			lite: want{
				sql: `WITH RECURSIVE "x" AS NOT MATERIALIZED (SELECT * FROM "t" UNION ALL SELECT * FROM "x") SELECT * FROM "x"`,
			},
		},
	})
}

func TestCTEWrites(t *testing.T) {
	ids := func(d db.Dialect) *db.SelectBuilder {
		return db.Use(d).Select("id").From("users").Where("active", false)
	}
	runStmts(t, []stmtCase{
		{
			name: "insert using a select with a cte",
			build: func(d db.Dialect) (string, []any, error) {
				sub := db.Use(d).
					Select("name").
					From("au").
					With("au", db.Use(d).Select().From("users").Where("active", true))
				return db.Use(d).Insert("archive").Using([]string{"name"}, sub).ToSQL()
			},
			pg:   `INSERT INTO "archive" ("name") WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1) SELECT "name" FROM "au"`,
			my:   "INSERT INTO `archive` (`name`) WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?) SELECT `name` FROM `au`",
			lite: `INSERT INTO "archive" ("name") WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?) SELECT "name" FROM "au"`,
			args: []any{true},
		},
		{
			name: "insert builder with moves the cte into the using select",
			build: func(d db.Dialect) (string, []any, error) {
				sub := db.Use(d).Select("name").From("au").With("own", db.Use(d).Select().From("posts").Where("n", 2))
				return db.Use(d).Insert("archive").
					With("au", db.Use(d).Select().From("users").Where("active", true)).
					Using([]string{"name"}, sub).ToSQL()
			},
			pg:   `INSERT INTO "archive" ("name") WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1), "own" AS (SELECT * FROM "posts" WHERE "n" = $2) SELECT "name" FROM "au"`,
			my:   "INSERT INTO `archive` (`name`) WITH `au` AS (SELECT * FROM `users` WHERE `active` = ?), `own` AS (SELECT * FROM `posts` WHERE `n` = ?) SELECT `name` FROM `au`",
			lite: `INSERT INTO "archive" ("name") WITH "au" AS (SELECT * FROM "users" WHERE "active" = ?), "own" AS (SELECT * FROM "posts" WHERE "n" = ?) SELECT "name" FROM "au"`,
			args: []any{true, 2},
		},
		{
			name: "update with a cte",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Update("posts").
					With("gone", ids(d)).
					Set("hidden", true).
					WhereIn("uid", db.Use(d).Select("id").From("gone")).ToSQL()
			},
			pg:    `WITH "gone" AS (SELECT "id" FROM "users" WHERE "active" = $1) UPDATE "posts" SET "hidden" = $2 WHERE "uid" IN (SELECT "id" FROM "gone")`,
			my:    "WITH `gone` AS (SELECT `id` FROM `users` WHERE `active` = ?) UPDATE `posts` SET `hidden` = ? WHERE `uid` IN (SELECT `id` FROM `gone`)",
			lite:  `WITH "gone" AS (SELECT "id" FROM "users" WHERE "active" = ?) UPDATE "posts" SET "hidden" = ? WHERE "uid" IN (SELECT "id" FROM "gone")`,
			maria: unsupported,
			args:  []any{false, true},
		},
		{
			name: "delete with a cte",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Delete("posts").
					With("gone", ids(d)).
					WhereIn("uid", db.Use(d).Select("id").From("gone")).ToSQL()
			},
			pg:    `WITH "gone" AS (SELECT "id" FROM "users" WHERE "active" = $1) DELETE FROM "posts" WHERE "uid" IN (SELECT "id" FROM "gone")`,
			my:    "WITH `gone` AS (SELECT `id` FROM `users` WHERE `active` = ?) DELETE FROM `posts` WHERE `uid` IN (SELECT `id` FROM `gone`)",
			lite:  `WITH "gone" AS (SELECT "id" FROM "users" WHERE "active" = ?) DELETE FROM "posts" WHERE "uid" IN (SELECT "id" FROM "gone")`,
			maria: unsupported,
			args:  []any{false},
		},
		{
			name: "update with a cte and a join",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Update("posts").
					With("gone", ids(d)).
					Join("gone", "gone.id", "posts.uid").
					Set("hidden", true).
					Where("posts.draft", false).ToSQL()
			},
			pg:    `WITH "gone" AS (SELECT "id" FROM "users" WHERE "active" = $1) UPDATE "posts" SET "hidden" = $2 WHERE "ctid" IN (SELECT "posts"."ctid" FROM "posts" INNER JOIN "gone" ON "gone"."id" = "posts"."uid" WHERE "posts"."draft" = $3)`,
			my:    "WITH `gone` AS (SELECT `id` FROM `users` WHERE `active` = ?) UPDATE `posts` INNER JOIN `gone` ON `gone`.`id` = `posts`.`uid` SET `hidden` = ? WHERE `posts`.`draft` = ?",
			lite:  `WITH "gone" AS (SELECT "id" FROM "users" WHERE "active" = ?) UPDATE "posts" SET "hidden" = ? WHERE "rowid" IN (SELECT "posts"."rowid" FROM "posts" INNER JOIN "gone" ON "gone"."id" = "posts"."uid" WHERE "posts"."draft" = ?)`,
			maria: unsupported,
			args:  []any{false, true, false},
		},
	})
}

func TestCTEWriteRejections(t *testing.T) {
	ins := func() *db.InsertBuilder {
		return db.Use(db.Postgres).Insert("t").With("c", db.Use(db.Postgres).Select().From("x"))
	}
	row := map[string]any{"a": 1}
	tests := []struct {
		name  string
		build func() (string, []any, error)
		want  error
	}{
		{"insert values", func() (string, []any, error) { return ins().Values(row).ToSQL() }, db.ErrUnsupported},
		{
			"insert ignore",
			func() (string, []any, error) { return ins().Ignore().Values(row).ToSQL() },
			db.ErrUnsupported,
		},
		{
			"upsert",
			func() (string, []any, error) { return ins().Values(row).Upsert([]string{"a"}, nil).ToSQL() },
			db.ErrUnsupported,
		},
		{
			"using select repeats the name",
			func() (string, []any, error) {
				sub := db.Use(db.Postgres).Select().From("c")
				return ins().Using(nil, sub.With("c", sub.Clone())).ToSQL()
			},
			db.ErrInvalidArgs,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestCTEErrors(t *testing.T) {
	d := db.Postgres
	x := func() *db.SelectBuilder { return db.Use(d).Select().From("x") }
	tests := []struct {
		name  string
		build *db.SelectBuilder
		want  error
	}{
		{"empty name", x().With("", x()), db.ErrInvalidArgs},
		{"qualified name", x().With("s.x", x()), db.ErrInvalidArgs},
		{"aliased name", x().With("x as y", x()), db.ErrInvalidArgs},
		{"nil query", x().With("c", nil), db.ErrInvalidArgs},
		{"empty column", x().With("c", x(), db.CTEColumns("a", "")), db.ErrInvalidArgs},
		{"nil option is ignored then body checked", x().With("c", nil, nil), db.ErrInvalidArgs},
		{
			"materialized and not materialized",
			x().With("c", x(), db.CTEMaterialized(), db.CTENotMaterialized()),
			db.ErrInvalidArgs,
		},
		{"union option without recursion", x().With("c", x(), db.CTEUnionDistinct()), db.ErrInvalidArgs},
		{"nil recursive member", x().WithRecursive("c", x(), nil), db.ErrInvalidArgs},
		{"nil recursive anchor", x().WithRecursive("c", nil, x()), db.ErrInvalidArgs},
		{"anchor with unions", x().WithRecursive("c", x().UnionAll(x()), x()), db.ErrInvalidArgs},
		{"duplicate name", x().With("c", x()).WithRaw("c", db.Raw("SELECT 1")), db.ErrInvalidArgs},
		{"error inside cte", x().With("c", db.Use(d).Select()), db.ErrNoTable},
		{"error inside recursive member", x().WithRecursive("c", x(), db.Use(d).Select()), db.ErrNoTable},
		{"error inside recursive anchor", x().WithRecursive("c", db.Use(d).Select(), x()), db.ErrNoTable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}

func TestCTESnapshotAndClone(t *testing.T) {
	sub := db.Use(db.Postgres).Select().From("users")
	base := db.Use(db.Postgres).Select().From("c").With("c", sub)
	sub.Where("late", 1)
	clone := base.Clone().With("d", db.Use(db.Postgres).Select().From("posts"))

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
