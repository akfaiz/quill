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
	// select "u"."id", "u"."name" from "users" as "u" inner join "posts" as "p" on "p"."user_id" = "u"."id" where "u"."votes" > $1 or "u"."name" = $2 order by "u"."created_at" desc limit 10
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
	// select * from `users` where `active` = ? and (`deleted_at` is null or `role` in (?, ?))
	// [true admin staff]
}

func ExampleBuilder_WhereIn_subquery() {
	banned := db.Use(db.Postgres).Table("bans").Select("user_id").Where("reason", "spam")
	sql, args, err := db.Use(db.Postgres).Table("users").WhereIn("id", banned).ToSQL()
	show(sql, args, err)
	// Output:
	// select * from "users" where "id" in (select "user_id" from "bans" where "reason" = $1)
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
	// (select "id" from "a") union all (select "id" from "b") order by "id" asc limit 5
	// []
}

func ExampleBuilder_UpsertSQL() {
	rows := []map[string]any{{"email": "a@example.com", "name": "A"}}
	sql, args, err := db.Use(db.Postgres).Table("users").
		UpsertSQL(rows, []string{"email"}, []string{"name"})
	show(sql, args, err)
	// Output:
	// insert into "users" ("email", "name") values ($1, $2) on conflict ("email") do update set "name" = "excluded"."name"
	// [a@example.com A]
}

func ExampleBuilder_UpdateSQL() {
	sql, args, err := db.Use(db.Postgres).Table("users").
		Where("id", 7).
		UpdateSQL(map[string]any{"name": "Ann", "options->theme": "dark"})
	show(sql, args, err)
	// Output:
	// update "users" set "name" = $1, "options" = jsonb_set("options"::jsonb, '{"theme"}', $2) where "id" = $3
	// [Ann "dark" 7]
}

func ExampleBuilder_UpdateSQL_guard() {
	_, _, err := db.Use(db.Postgres).Table("users").UpdateSQL(map[string]any{"active": false})
	fmt.Println(err)
	sql, _, _ := db.Use(db.Postgres).Table("users").AllRows().UpdateSQL(map[string]any{"active": false})
	fmt.Println(sql)
	// Output:
	// quill: update/delete without a where clause; call AllRows() to confirm
	// update "users" set "active" = $1
}

func ExampleBuilder_WhereJsonContains() {
	for _, d := range []db.Dialect{db.Postgres, db.MySQL, db.SQLite} {
		sql, _, _ := db.Use(d).Table("users").WhereJsonContains("options->tags", "go").ToSQL()
		fmt.Println(sql)
	}
	// Output:
	// select * from "users" where ("options"->'tags')::jsonb @> $1
	// select * from `users` where json_contains(`options`, ?, '$."tags"')
	// select * from "users" where exists (select 1 from json_each("options", '$."tags"') where "json_each"."value" is ?)
}

func ExampleBuilder_ToRawSQL() {
	// For debugging only: the result is not injection-safe.
	sql, _ := db.Use(db.Postgres).Table("users").Where("name", "o'brien").Where("age", ">", 30).ToRawSQL()
	fmt.Println(sql)
	// Output:
	// select * from "users" where "name" = 'o''brien' and "age" > 30
}

func ExampleRaw() {
	sql, args, err := db.Use(db.Postgres).Table("orders").
		SelectRaw("date_trunc('day', created_at) as day").
		SelectRaw("sum(total) filter (where status = ?) as paid", "paid").
		GroupByRaw("1").
		ToSQL()
	show(sql, args, err)
	// Output:
	// select date_trunc('day', created_at) as day, sum(total) filter (where status = $1) as paid from "orders" group by 1
	// [paid]
}

func ExampleBuilder_MustSQL() {
	// For fixed queries where an error would be a programming mistake.
	sql, args := db.Use(db.Postgres).Table("users").Where("id", 7).MustSQL()
	fmt.Println(sql, args)
	// Output:
	// select * from "users" where "id" = $1 [7]
}

func ExampleBuilder_MustRawSQL() {
	fmt.Println(db.Use(db.Postgres).Table("users").Where("name", "ann").MustRawSQL())
	// Output:
	// select * from "users" where "name" = 'ann'
}
