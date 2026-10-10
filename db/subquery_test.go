package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/akfaiz/quill/db"
)

func TestSubqueryAndUnionGolden(t *testing.T) {
	tests := []golden{
		{
			name: "select sub and from sub",
			build: func(d db.Dialect) *db.Builder {
				cnt := db.Use(d).
					Table("posts").
					SelectRaw("count(*)").
					WhereColumn("posts.uid", "users.id").
					Where("pub", true)
				inner := db.Use(d).Table("users").Select("id", "name").Where("age", ">", 18)
				return db.NewBuilder(d).FromSub(inner, "u").SelectSub(cnt, "post_count").Where("u.id", 5)
			},
			pg:   `SELECT (SELECT count(*) FROM "posts" WHERE "posts"."uid" = "users"."id" AND "pub" = $1) AS "post_count" FROM (SELECT "id", "name" FROM "users" WHERE "age" > $2) AS "u" WHERE "u"."id" = $3`,
			my:   "SELECT (SELECT count(*) FROM `posts` WHERE `posts`.`uid` = `users`.`id` AND `pub` = ?) AS `post_count` FROM (SELECT `id`, `name` FROM `users` WHERE `age` > ?) AS `u` WHERE `u`.`id` = ?",
			lite: `SELECT (SELECT count(*) FROM "posts" WHERE "posts"."uid" = "users"."id" AND "pub" = ?) AS "post_count" FROM (SELECT "id", "name" FROM "users" WHERE "age" > ?) AS "u" WHERE "u"."id" = ?`,
			args: []any{true, 18, 5},
		},
		{
			name: "from raw",
			build: func(d db.Dialect) *db.Builder {
				return db.NewBuilder(d).FromRaw("generate_series(?, ?) g", 1, 3).Where("g", ">", 1)
			},
			pg:   `SELECT * FROM generate_series($1, $2) g WHERE "g" > $3`,
			my:   "SELECT * FROM generate_series(?, ?) g WHERE `g` > ?",
			lite: `SELECT * FROM generate_series(?, ?) g WHERE "g" > ?`,
			args: []any{1, 3, 1},
		},
		{
			name: "union and union all with outer order limit offset",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("a").Select("id").Where("x", 1).
					Union(db.Use(d).Table("b").Select("id").Where("y", 2)).
					UnionAll(db.Use(d).Table("c").Select("id")).
					OrderBy("id").Limit(5).Offset(10)
			},
			pg:   `(SELECT "id" FROM "a" WHERE "x" = $1) UNION (SELECT "id" FROM "b" WHERE "y" = $2) UNION ALL (SELECT "id" FROM "c") ORDER BY "id" ASC LIMIT 5 OFFSET 10`,
			my:   "(SELECT `id` FROM `a` WHERE `x` = ?) UNION (SELECT `id` FROM `b` WHERE `y` = ?) UNION ALL (SELECT `id` FROM `c`) ORDER BY `id` ASC LIMIT 5 OFFSET 10",
			lite: `SELECT * FROM (SELECT "id" FROM "a" WHERE "x" = ?) UNION SELECT * FROM (SELECT "id" FROM "b" WHERE "y" = ?) UNION ALL SELECT * FROM (SELECT "id" FROM "c") ORDER BY "id" ASC LIMIT 5 OFFSET 10`,
			args: []any{1, 2},
		},
		{
			name: "order and limit before union stay on main query",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("a").OrderBy("n").Limit(1).Union(db.Use(d).Table("b"))
			},
			pg:   `(SELECT * FROM "a" ORDER BY "n" ASC LIMIT 1) UNION (SELECT * FROM "b")`,
			my:   "(SELECT * FROM `a` ORDER BY `n` ASC LIMIT 1) UNION (SELECT * FROM `b`)",
			lite: `SELECT * FROM (SELECT * FROM "a" ORDER BY "n" ASC LIMIT 1) UNION SELECT * FROM (SELECT * FROM "b")`,
		},
		{
			name: "union is a snapshot",
			build: func(d db.Dialect) *db.Builder {
				u := db.Use(d).Table("b")
				b := db.Use(d).Table("a").Union(u)
				u.Where("late", 1)
				return b
			},
			pg:   `(SELECT * FROM "a") UNION (SELECT * FROM "b")`,
			my:   "(SELECT * FROM `a`) UNION (SELECT * FROM `b`)",
			lite: `SELECT * FROM (SELECT * FROM "a") UNION SELECT * FROM (SELECT * FROM "b")`,
		},
	}
	runGolden(t, tests)
}

func TestSubqueryAndUnionErrors(t *testing.T) {
	d := db.MySQL
	tests := []struct {
		name  string
		build *db.Builder
		want  error
	}{
		{"FROM sub needs alias", db.NewBuilder(d).FromSub(db.Use(d).Table("x"), ""), db.ErrInvalidArgs},
		{"UNION member error", db.Use(d).Table("a").Union(db.NewBuilder(d)), db.ErrNoTable},
		{"SELECT sub error", db.Use(d).Table("a").SelectSub(db.NewBuilder(d), "s"), db.ErrNoTable},
		{"nil SELECT sub", db.Use(d).Table("a").SelectSub(nil, "s"), db.ErrInvalidArgs},
		{"nil FROM sub", db.NewBuilder(d).FromSub(nil, "s"), db.ErrInvalidArgs},
		{"nil UNION member", db.Use(d).Table("a").Union(nil), db.ErrInvalidArgs},
		{"nil UNION ALL member", db.Use(d).Table("a").UnionAll(nil), db.ErrInvalidArgs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}
