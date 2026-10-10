package db_test

import (
	"fmt"

	"github.com/akfaiz/quill/db"
)

func show(sql string, args []any, err error) {
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(sql)
	fmt.Println(args)
}

func ExampleUse() {
	sql, args, err := db.Use(db.Postgres).Table("users as u").
		Select("u.id", "u.name").
		Join("posts as p", "p.user_id", "u.id").
		Where("u.votes", ">", 100).
		OrWhere("u.name", "john").
		OrderByDesc("u.created_at").
		Limit(10).
		ToSQL()
	show(sql, args, err)
	// Output:
	// SELECT "u"."id", "u"."name" FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" WHERE "u"."votes" > $1 OR "u"."name" = $2 ORDER BY "u"."created_at" DESC LIMIT 10
	// [100 john]
}

func ExampleBuilder_WhereGroup() {
	sql, args, err := db.Use(db.MySQL).Table("users").
		Where("active", true).
		WhereGroup(func(q *db.Builder) {
			q.WhereNull("deleted_at").OrWhereIn("role", []string{"admin", "staff"})
		}).
		ToSQL()
	show(sql, args, err)
	// Output:
	// SELECT * FROM `users` WHERE `active` = ? AND (`deleted_at` IS NULL OR `role` IN (?, ?))
	// [true admin staff]
}

func ExampleBuilder_WhereIn_subquery() {
	banned := db.Use(db.Postgres).Table("bans").Select("user_id").Where("reason", "spam")
	sql, args, err := db.Use(db.Postgres).Table("users").WhereIn("id", banned).ToSQL()
	show(sql, args, err)
	// Output:
	// SELECT * FROM "users" WHERE "id" IN (SELECT "user_id" FROM "bans" WHERE "reason" = $1)
	// [spam]
}

func ExampleBuilder_Union() {
	sql, _, err := db.Use(db.Postgres).Table("a").Select("id").
		UnionAll(db.Use(db.Postgres).Table("b").Select("id")).
		OrderBy("id").
		Limit(5).
		ToSQL()
	show(sql, nil, err)
	// Output:
	// (SELECT "id" FROM "a") UNION ALL (SELECT "id" FROM "b") ORDER BY "id" ASC LIMIT 5
	// []
}

func ExampleBuilder_UpsertSQL() {
	rows := []map[string]any{{"email": "a@example.com", "name": "A"}}
	sql, args, err := db.Use(db.Postgres).Table("users").
		UpsertSQL(rows, []string{"email"}, []string{"name"})
	show(sql, args, err)
	// Output:
	// INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"
	// [a@example.com A]
}

func ExampleBuilder_UpdateSQL() {
	sql, args, err := db.Use(db.Postgres).Table("users").
		Where("id", 7).
		UpdateSQL(map[string]any{"name": "Ann", "options->theme": "dark"})
	show(sql, args, err)
	// Output:
	// UPDATE "users" SET "name" = $1, "options" = JSONB_SET("options"::JSONB, '{"theme"}', $2) WHERE "id" = $3
	// [Ann "dark" 7]
}

func ExampleBuilder_UpdateSQL_guard() {
	_, _, err := db.Use(db.Postgres).Table("users").UpdateSQL(map[string]any{"active": false})
	fmt.Println(err)
	sql, _, _ := db.Use(db.Postgres).Table("users").AllRows().UpdateSQL(map[string]any{"active": false})
	fmt.Println(sql)
	// Output:
	// quill: update/delete without a where clause; call AllRows() to confirm
	// UPDATE "users" SET "active" = $1
}

func ExampleBuilder_WhereJsonContains() {
	for _, d := range []db.Dialect{db.Postgres, db.MySQL, db.SQLite} {
		sql, _, _ := db.Use(d).Table("users").WhereJsonContains("options->tags", "go").ToSQL()
		fmt.Println(sql)
	}
	// Output:
	// SELECT * FROM "users" WHERE ("options"->'tags')::JSONB @> $1
	// SELECT * FROM `users` WHERE JSON_CONTAINS(`options`, ?, '$."tags"')
	// SELECT * FROM "users" WHERE EXISTS (SELECT 1 FROM JSON_EACH("options", '$."tags"') WHERE "json_each"."value" IS ?)
}

func ExampleBuilder_ToRawSQL() {
	// For debugging only: the result is not injection-safe.
	sql, _ := db.Use(db.Postgres).Table("users").Where("name", "o'brien").Where("age", ">", 30).ToRawSQL()
	fmt.Println(sql)
	// Output:
	// SELECT * FROM "users" WHERE "name" = 'o''brien' AND "age" > 30
}

func ExampleRaw() {
	sql, args, err := db.Use(db.Postgres).Table("orders").
		SelectRaw("date_trunc('day', created_at) AS day").
		SelectRaw("sum(total) filter (WHERE status = ?) AS paid", "paid").
		GroupByRaw("1").
		ToSQL()
	show(sql, args, err)
	// Output:
	// SELECT date_trunc('day', created_at) AS day, sum(total) filter (WHERE status = $1) AS paid FROM "orders" GROUP BY 1
	// [paid]
}

func ExampleBuilder_MustSQL() {
	// For fixed queries where an error would be a programming mistake.
	sql, args := db.Use(db.Postgres).Table("users").Where("id", 7).MustSQL()
	fmt.Println(sql, args)
	// Output:
	// SELECT * FROM "users" WHERE "id" = $1 [7]
}

func ExampleBuilder_MustRawSQL() {
	fmt.Println(db.Use(db.Postgres).Table("users").Where("name", "ann").MustRawSQL())
	// Output:
	// SELECT * FROM "users" WHERE "name" = 'ann'
}
