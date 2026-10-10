package db_test

import (
	"testing"

	"github.com/akfaiz/quill/db"
)

func TestOrderingGolden(t *testing.T) {
	tests := []golden{
		{
			name: "latest oldest default and custom",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").Latest().Oldest("updated_at").Latest("x")
			},
			pg:   `select * from "t" order by "created_at" desc, "updated_at" asc, "x" desc`,
			my:   "select * from `t` order by `created_at` desc, `updated_at` asc, `x` desc",
			lite: `select * from "t" order by "created_at" desc, "updated_at" asc, "x" desc`,
		},
		{
			name:  "random order",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").InRandomOrder() },
			pg:    `select * from "t" order by RANDOM()`,
			my:    "select * from `t` order by RAND()",
			lite:  `select * from "t" order by RANDOM()`,
		},
		{
			name:  "random order seeded",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").InRandomOrder(42) },
			pg:    `select * from "t" order by RANDOM()`,
			my:    "select * from `t` order by RAND(42)",
			lite:  `select * from "t" order by RANDOM()`,
		},
		{
			name: "reorder and reorder by",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").OrderBy("a").OrderByRaw("b").Reorder().OrderBy("c").ReorderBy("d", "desc")
			},
			pg:   `select * from "t" order by "d" desc`,
			my:   "select * from `t` order by `d` desc",
			lite: `select * from "t" order by "d" desc`,
		},
		{
			name: "keyset paging replaces existing id order",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").OrderBy("id", "desc").OrderBy("n").ForPageAfterID(15, 100)
			},
			pg:   `select * from "t" where "id" > $1 order by "n" asc, "id" asc limit 15`,
			my:   "select * from `t` where `id` > ? order by `n` asc, `id` asc limit 15",
			lite: `select * from "t" where "id" > ? order by "n" asc, "id" asc limit 15`,
			args: []any{100},
		},
		{
			name: "keyset paging before and nil last id",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").ForPageBeforeID(5, nil, "uid")
			},
			pg:   `select * from "t" where "uid" is not null order by "uid" desc limit 5`,
			my:   "select * from `t` where `uid` is not null order by `uid` desc limit 5",
			lite: `select * from "t" where "uid" is not null order by "uid" desc limit 5`,
		},
	}
	runGolden(t, tests)
}
