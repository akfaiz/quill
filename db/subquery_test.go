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
			pg:   `select (select count(*) from "posts" where "posts"."uid" = "users"."id" and "pub" = $1) as "post_count" from (select "id", "name" from "users" where "age" > $2) as "u" where "u"."id" = $3`,
			my:   "select (select count(*) from `posts` where `posts`.`uid` = `users`.`id` and `pub` = ?) as `post_count` from (select `id`, `name` from `users` where `age` > ?) as `u` where `u`.`id` = ?",
			lite: `select (select count(*) from "posts" where "posts"."uid" = "users"."id" and "pub" = ?) as "post_count" from (select "id", "name" from "users" where "age" > ?) as "u" where "u"."id" = ?`,
			args: []any{true, 18, 5},
		},
		{
			name: "from raw",
			build: func(d db.Dialect) *db.Builder {
				return db.NewBuilder(d).FromRaw("generate_series(?, ?) g", 1, 3).Where("g", ">", 1)
			},
			pg:   `select * from generate_series($1, $2) g where "g" > $3`,
			my:   "select * from generate_series(?, ?) g where `g` > ?",
			lite: `select * from generate_series(?, ?) g where "g" > ?`,
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
			pg:   `(select "id" from "a" where "x" = $1) union (select "id" from "b" where "y" = $2) union all (select "id" from "c") order by "id" asc limit 5 offset 10`,
			my:   "(select `id` from `a` where `x` = ?) union (select `id` from `b` where `y` = ?) union all (select `id` from `c`) order by `id` asc limit 5 offset 10",
			lite: `select * from (select "id" from "a" where "x" = ?) union select * from (select "id" from "b" where "y" = ?) union all select * from (select "id" from "c") order by "id" asc limit 5 offset 10`,
			args: []any{1, 2},
		},
		{
			name: "order and limit before union stay on main query",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("a").OrderBy("n").Limit(1).Union(db.Use(d).Table("b"))
			},
			pg:   `(select * from "a" order by "n" asc limit 1) union (select * from "b")`,
			my:   "(select * from `a` order by `n` asc limit 1) union (select * from `b`)",
			lite: `select * from (select * from "a" order by "n" asc limit 1) union select * from (select * from "b")`,
		},
		{
			name: "union is a snapshot",
			build: func(d db.Dialect) *db.Builder {
				u := db.Use(d).Table("b")
				b := db.Use(d).Table("a").Union(u)
				u.Where("late", 1)
				return b
			},
			pg:   `(select * from "a") union (select * from "b")`,
			my:   "(select * from `a`) union (select * from `b`)",
			lite: `select * from (select * from "a") union select * from (select * from "b")`,
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
		{"from sub needs alias", db.NewBuilder(d).FromSub(db.Use(d).Table("x"), ""), db.ErrInvalidArgs},
		{"union member error", db.Use(d).Table("a").Union(db.NewBuilder(d)), db.ErrNoTable},
		{"select sub error", db.Use(d).Table("a").SelectSub(db.NewBuilder(d), "s"), db.ErrNoTable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			assert.ErrorIs(t, err, tc.want)
		})
	}
}
