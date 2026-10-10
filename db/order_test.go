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
			pg:   `SELECT * FROM "t" ORDER BY "created_at" DESC, "updated_at" ASC, "x" DESC`,
			my:   "SELECT * FROM `t` ORDER BY `created_at` DESC, `updated_at` ASC, `x` DESC",
			lite: `SELECT * FROM "t" ORDER BY "created_at" DESC, "updated_at" ASC, "x" DESC`,
		},
		{
			name:  "random order",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").InRandomOrder() },
			pg:    `SELECT * FROM "t" ORDER BY RANDOM()`,
			my:    "SELECT * FROM `t` ORDER BY RAND()",
			lite:  `SELECT * FROM "t" ORDER BY RANDOM()`,
		},
		{
			name:  "random order seeded",
			build: func(d db.Dialect) *db.Builder { return db.Use(d).Table("t").InRandomOrder(42) },
			pg:    `SELECT * FROM "t" ORDER BY RANDOM()`,
			my:    "SELECT * FROM `t` ORDER BY RAND(42)",
			lite:  `SELECT * FROM "t" ORDER BY RANDOM()`,
		},
		{
			name: "reorder and reorder by",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").OrderBy("a").OrderByRaw("b").Reorder().OrderBy("c").ReorderBy("d", "desc")
			},
			pg:   `SELECT * FROM "t" ORDER BY "d" DESC`,
			my:   "SELECT * FROM `t` ORDER BY `d` DESC",
			lite: `SELECT * FROM "t" ORDER BY "d" DESC`,
		},
		{
			name: "keyset paging replaces existing id order",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").OrderBy("id", "desc").OrderBy("n").ForPageAfterID(15, 100)
			},
			pg:   `SELECT * FROM "t" WHERE "id" > $1 ORDER BY "n" ASC, "id" ASC LIMIT 15`,
			my:   "SELECT * FROM `t` WHERE `id` > ? ORDER BY `n` ASC, `id` ASC LIMIT 15",
			lite: `SELECT * FROM "t" WHERE "id" > ? ORDER BY "n" ASC, "id" ASC LIMIT 15`,
			args: []any{100},
		},
		{
			name: "keyset paging before and nil last id",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").ForPageBeforeID(5, nil, "uid")
			},
			pg:   `SELECT * FROM "t" WHERE "uid" IS NOT NULL ORDER BY "uid" DESC LIMIT 5`,
			my:   "SELECT * FROM `t` WHERE `uid` IS NOT NULL ORDER BY `uid` DESC LIMIT 5",
			lite: `SELECT * FROM "t" WHERE "uid" IS NOT NULL ORDER BY "uid" DESC LIMIT 5`,
		},
		{
			name: "keyset paging groups an existing OR",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").Where("a", 1).OrWhere("b", 2).ForPageAfterID(10, 5)
			},
			pg:   `SELECT * FROM "t" WHERE ("a" = $1 OR "b" = $2) AND "id" > $3 ORDER BY "id" ASC LIMIT 10`,
			my:   "SELECT * FROM `t` WHERE (`a` = ? OR `b` = ?) AND `id` > ? ORDER BY `id` ASC LIMIT 10",
			lite: `SELECT * FROM "t" WHERE ("a" = ? OR "b" = ?) AND "id" > ? ORDER BY "id" ASC LIMIT 10`,
			args: []any{1, 2, 5},
		},
		{
			name: "keyset paging groups a raw where",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").WhereRaw("a = ? OR b = ?", 1, 2).ForPageBeforeID(10, 5)
			},
			pg:   `SELECT * FROM "t" WHERE (a = $1 OR b = $2) AND "id" < $3 ORDER BY "id" DESC LIMIT 10`,
			my:   "SELECT * FROM `t` WHERE (a = ? OR b = ?) AND `id` < ? ORDER BY `id` DESC LIMIT 10",
			lite: `SELECT * FROM "t" WHERE (a = ? OR b = ?) AND "id" < ? ORDER BY "id" DESC LIMIT 10`,
			args: []any{1, 2, 5},
		},
		{
			name: "keyset paging leaves plain AND wheres ungrouped",
			build: func(d db.Dialect) *db.Builder {
				return db.Use(d).Table("t").Where("a", 1).Where("b", 2).ForPageAfterID(10, 5)
			},
			pg:   `SELECT * FROM "t" WHERE "a" = $1 AND "b" = $2 AND "id" > $3 ORDER BY "id" ASC LIMIT 10`,
			my:   "SELECT * FROM `t` WHERE `a` = ? AND `b` = ? AND `id` > ? ORDER BY `id` ASC LIMIT 10",
			lite: `SELECT * FROM "t" WHERE "a" = ? AND "b" = ? AND "id" > ? ORDER BY "id" ASC LIMIT 10`,
			args: []any{1, 2, 5},
		},
	}
	runGolden(t, tests)
}
