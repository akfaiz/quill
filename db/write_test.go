package db_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

const unsupported = "!unsupported"

type stmtCase struct {
	name  string
	build func(d db.Dialect) (string, []any, error)
	pg    string
	my    string
	lite  string
	maria string // MariaDB: same as my unless set
	args  []any  // same for every dialect unless argsPG is set
}

// orMy returns the MariaDB override, or the MySQL expectation when there is none.
func orMy(override, my string) string {
	if override != "" {
		return override
	}
	return my
}

func runStmts(t *testing.T, cases []stmtCase) {
	t.Helper()
	for _, tc := range cases {
		for _, dl := range []struct {
			d    db.Dialect
			want string
		}{{db.Postgres, tc.pg}, {db.MySQL, tc.my}, {db.SQLite, tc.lite}, {db.MariaDB, orMy(tc.maria, tc.my)}} {
			t.Run(tc.name+"/"+dl.d.String(), func(t *testing.T) {
				sql, args, err := tc.build(dl.d)
				if dl.want == unsupported {
					require.ErrorIs(t, err, db.ErrUnsupported, "sql=%q", sql)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, dl.want, sql)
				if len(args) == 0 && len(tc.args) == 0 {
					return
				}
				assert.Equal(t, tc.args, args)
			})
		}
	}
}

func m(kv ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i < len(kv); i += 2 {
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

func TestInsertGolden(t *testing.T) {
	runStmts(t, []stmtCase{
		{
			name: "single row sorted columns",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").InsertSQL(m("name", "ann", "age", 30))
			},
			pg:   `INSERT INTO "users" ("age", "name") VALUES ($1, $2)`,
			my:   "INSERT INTO `users` (`age`, `name`) VALUES (?, ?)",
			lite: `INSERT INTO "users" ("age", "name") VALUES (?, ?)`,
			args: []any{30, "ann"},
		},
		{
			name: "batch with expression and subquery values",
			build: func(d db.Dialect) (string, []any, error) {
				sub := db.Use(d).Table("teams").Select("id").Where("slug", "x")
				return db.Use(d).Table("users").InsertSQL(
					m("a", 1, "b", db.Raw("now()")),
					m("a", 2, "b", sub),
				)
			},
			pg:   `INSERT INTO "users" ("a", "b") VALUES ($1, now()), ($2, (SELECT "id" FROM "teams" WHERE "slug" = $3))`,
			my:   "INSERT INTO `users` (`a`, `b`) VALUES (?, now()), (?, (SELECT `id` FROM `teams` WHERE `slug` = ?))",
			lite: `INSERT INTO "users" ("a", "b") VALUES (?, now()), (?, (SELECT "id" FROM "teams" WHERE "slug" = ?))`,
			args: []any{1, 2, "x"},
		},
		{
			name: "default values",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertSQL(m())
			},
			pg:   `INSERT INTO "t" DEFAULT VALUES`,
			my:   "INSERT INTO `t` () VALUES ()",
			lite: `INSERT INTO "t" DEFAULT VALUES`,
		},
		{
			name: "insert or ignore",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertOrIgnoreSQL(m("a", 1))
			},
			pg:   `INSERT INTO "t" ("a") VALUES ($1) ON CONFLICT DO NOTHING`,
			my:   "INSERT IGNORE INTO `t` (`a`) VALUES (?)",
			lite: `INSERT OR IGNORE INTO "t" ("a") VALUES (?)`,
			args: []any{1},
		},
		{
			name: "insert get id",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertGetIDSQL(m("a", 1))
			},
			pg:   `INSERT INTO "t" ("a") VALUES ($1) RETURNING "id"`,
			my:   "INSERT INTO `t` (`a`) VALUES (?)",
			lite: `INSERT INTO "t" ("a") VALUES (?)`,
			args: []any{1},
		},
		{
			name: "insert get id custom sequence",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertGetIDSQL(m("a", 1), "uid")
			},
			pg:   `INSERT INTO "t" ("a") VALUES ($1) RETURNING "uid"`,
			my:   "INSERT INTO `t` (`a`) VALUES (?)",
			lite: `INSERT INTO "t" ("a") VALUES (?)`,
			args: []any{1},
		},
		{
			name: "returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Returning("id", "name").InsertSQL(m("a", 1))
			},
			pg:   `INSERT INTO "t" ("a") VALUES ($1) RETURNING "id", "name"`,
			my:   unsupported,
			lite: `INSERT INTO "t" ("a") VALUES (?) RETURNING "id", "name"`,
			args: []any{1},
		},
		{
			name: "insert using",
			build: func(d db.Dialect) (string, []any, error) {
				src := db.Use(d).Table("old").Select("a", "b").Where("n", ">", 1)
				return db.Use(d).Table("new").InsertUsingSQL([]string{"a", "b"}, src)
			},
			pg:   `INSERT INTO "new" ("a", "b") SELECT "a", "b" FROM "old" WHERE "n" > $1`,
			my:   "INSERT INTO `new` (`a`, `b`) SELECT `a`, `b` FROM `old` WHERE `n` > ?",
			lite: `INSERT INTO "new" ("a", "b") SELECT "a", "b" FROM "old" WHERE "n" > ?`,
			args: []any{1},
		},
		{
			name: "insert or ignore using without column list",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("new").InsertOrIgnoreUsingSQL([]string{"*"}, db.Use(d).Table("old"))
			},
			pg:   `INSERT INTO "new" SELECT * FROM "old" ON CONFLICT DO NOTHING`,
			my:   "INSERT IGNORE INTO `new` SELECT * FROM `old`",
			lite: `INSERT OR IGNORE INTO "new" SELECT * FROM "old"`,
		},
	})
}

func TestUpsertGolden(t *testing.T) {
	rows := []map[string]any{m("email", "a@x", "name", "A"), m("email", "b@x", "name", "B")}
	runStmts(t, []stmtCase{
		{
			name: "explicit update columns",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").UpsertSQL(rows, []string{"email"}, []string{"name"})
			},
			pg:   `INSERT INTO "users" ("email", "name") VALUES ($1, $2), ($3, $4) ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"`,
			my:   "INSERT INTO `users` (`email`, `name`) VALUES (?, ?), (?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)",
			lite: `INSERT INTO "users" ("email", "name") VALUES (?, ?), (?, ?) ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"`,
			args: []any{"a@x", "A", "b@x", "B"},
		},
		{
			name: "nil update means all columns, returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").Returning("id").UpsertSQL(rows[:1], []string{"email"}, nil)
			},
			pg:   `INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ("email") DO UPDATE SET "email" = "excluded"."email", "name" = "excluded"."name" RETURNING "id"`,
			my:   unsupported,
			lite: `INSERT INTO "users" ("email", "name") VALUES (?, ?) ON CONFLICT ("email") DO UPDATE SET "email" = "excluded"."email", "name" = "excluded"."name" RETURNING "id"`,
			args: []any{"a@x", "A"},
		},
	})
}

func TestUpdateGolden(t *testing.T) {
	runStmts(t, []stmtCase{
		{
			name: "simple update binds values before where",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").Where("id", 7).UpdateSQL(m("name", "z", "age", 5))
			},
			pg:   `UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3`,
			my:   "UPDATE `users` SET `age` = ?, `name` = ? WHERE `id` = ?",
			lite: `UPDATE "users" SET "age" = ?, "name" = ? WHERE "id" = ?`,
			args: []any{5, "z", 7},
		},
		{
			name: "raw expression and returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("id", 1).Returning("*").UpdateSQL(m("n", db.Raw("n + ?", 2)))
			},
			pg:   `UPDATE "t" SET "n" = n + $1 WHERE "id" = $2 RETURNING *`,
			my:   unsupported,
			lite: `UPDATE "t" SET "n" = n + ? WHERE "id" = ? RETURNING *`,
			args: []any{2, 1},
		},
		{
			name: "update with join",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users as u").
					Join("orders as o", "o.uid", "u.id").
					Where("o.total", ">", 100).
					UpdateSQL(m("u.vip", true))
			},
			pg:   `UPDATE "users" AS "u" SET "vip" = $1 WHERE "ctid" IN (SELECT "u"."ctid" FROM "users" AS "u" INNER JOIN "orders" AS "o" ON "o"."uid" = "u"."id" WHERE "o"."total" > $2)`,
			my:   "UPDATE `users` AS `u` INNER JOIN `orders` AS `o` ON `o`.`uid` = `u`.`id` SET `u`.`vip` = ? WHERE `o`.`total` > ?",
			lite: `UPDATE "users" AS "u" SET "vip" = ? WHERE "rowid" IN (SELECT "u"."rowid" FROM "users" AS "u" INNER JOIN "orders" AS "o" ON "o"."uid" = "u"."id" WHERE "o"."total" > ?)`,
			args: []any{true, 100},
		},
		{
			name: "update with limit",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("a", 1).OrderBy("id").Limit(2).UpdateSQL(m("b", 2))
			},
			pg:   `UPDATE "t" SET "b" = $1 WHERE "ctid" IN (SELECT "t"."ctid" FROM "t" WHERE "a" = $2 ORDER BY "id" ASC LIMIT 2)`,
			my:   "UPDATE `t` SET `b` = ? WHERE `a` = ? ORDER BY `id` ASC LIMIT 2",
			lite: `UPDATE "t" SET "b" = ? WHERE "rowid" IN (SELECT "t"."rowid" FROM "t" WHERE "a" = ? ORDER BY "id" ASC LIMIT 2)`,
			args: []any{2, 1},
		},
		{
			name: "update all rows when confirmed",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").AllRows().UpdateSQL(m("a", 1))
			},
			pg:   `UPDATE "t" SET "a" = $1`,
			my:   "UPDATE `t` SET `a` = ?",
			lite: `UPDATE "t" SET "a" = ?`,
			args: []any{1},
		},
		{
			name: "update from",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").
					Join("accounts", "accounts.uid", "users.id").
					Where("accounts.active", true).
					UpdateFromSQL(m("plan", db.Raw(`"accounts"."plan"`)))
			},
			pg:   `UPDATE "users" SET "plan" = "accounts"."plan" FROM "accounts" WHERE "accounts"."active" = $1 AND "accounts"."uid" = "users"."id"`,
			my:   unsupported,
			lite: unsupported,
			args: []any{true},
		},
		{
			name: "update from groups an OR in the wheres and in the join conditions",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("posts").
					JoinFunc("users", func(j *db.JoinClause) {
						j.On("users.id", "posts.user_id").OrOn("users.id", "posts.editor_id")
					}).
					Where("users.banned", true).OrWhere("users.spam", true).
					UpdateFromSQL(m("hidden", true))
			},
			pg: `UPDATE "posts" SET "hidden" = $1 FROM "users" ` +
				`WHERE ("users"."banned" = $2 OR "users"."spam" = $3) ` +
				`AND ("users"."id" = "posts"."user_id" OR "users"."id" = "posts"."editor_id")`,
			my:   unsupported,
			lite: unsupported,
			args: []any{true, true, true},
		},
		{
			name: "update with join and limit",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("posts").
					Join("users", "users.id", "posts.user_id").
					OrderBy("posts.id").Limit(1).
					UpdateSQL(m("hidden", true))
			},
			pg:   `UPDATE "posts" SET "hidden" = $1 WHERE "ctid" IN (SELECT "posts"."ctid" FROM "posts" INNER JOIN "users" ON "users"."id" = "posts"."user_id" ORDER BY "posts"."id" ASC LIMIT 1)`,
			my:   unsupported,
			lite: `UPDATE "posts" SET "hidden" = ? WHERE "rowid" IN (SELECT "posts"."rowid" FROM "posts" INNER JOIN "users" ON "users"."id" = "posts"."user_id" ORDER BY "posts"."id" ASC LIMIT 1)`,
			args: []any{true},
		},
		{
			name: "update with join and order by",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("posts").
					Join("users", "users.id", "posts.user_id").
					Where("users.banned", true).OrderBy("posts.id").
					UpdateSQL(m("hidden", true))
			},
			pg:   `UPDATE "posts" SET "hidden" = $1 WHERE "ctid" IN (SELECT "posts"."ctid" FROM "posts" INNER JOIN "users" ON "users"."id" = "posts"."user_id" WHERE "users"."banned" = $2 ORDER BY "posts"."id" ASC)`,
			my:   unsupported,
			lite: `UPDATE "posts" SET "hidden" = ? WHERE "rowid" IN (SELECT "posts"."rowid" FROM "posts" INNER JOIN "users" ON "users"."id" = "posts"."user_id" WHERE "users"."banned" = ? ORDER BY "posts"."id" ASC)`,
			args: []any{true, true},
		},
		{
			name: "update from without base where uses join conditions",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").Join("a", "a.uid", "users.id").UpdateFromSQL(m("x", 1))
			},
			pg:   `UPDATE "users" SET "x" = $1 FROM "a" WHERE "a"."uid" = "users"."id"`,
			my:   unsupported,
			lite: unsupported,
			args: []any{1},
		},
	})
}

func TestIncrementGolden(t *testing.T) {
	runStmts(t, []stmtCase{
		{
			name: "increment with extra",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("posts").Where("id", 3).IncrementSQL("views", 1, m("seen_at", "now"))
			},
			pg:   `UPDATE "posts" SET "views" = "views" + $1, "seen_at" = $2 WHERE "id" = $3`,
			my:   "UPDATE `posts` SET `views` = `views` + ?, `seen_at` = ? WHERE `id` = ?",
			lite: `UPDATE "posts" SET "views" = "views" + ?, "seen_at" = ? WHERE "id" = ?`,
			args: []any{1, "now", 3},
		},
		{
			name: "decrement each with override",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).
					Table("stock").
					Where("sku", "a").
					DecrementEachSQL(m("qty", 2, "reserved", 1.5), m("reserved", 0))
			},
			pg:   `UPDATE "stock" SET "qty" = "qty" - $1, "reserved" = $2 WHERE "sku" = $3`,
			my:   "UPDATE `stock` SET `qty` = `qty` - ?, `reserved` = ? WHERE `sku` = ?",
			lite: `UPDATE "stock" SET "qty" = "qty" - ?, "reserved" = ? WHERE "sku" = ?`,
			args: []any{2, 0, "a"},
		},
		{
			name: "qualified column in increment (mysql join target)",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("id", 1).IncrementEachSQL(m("a", 1, "b", 2), nil)
			},
			pg:   `UPDATE "t" SET "a" = "a" + $1, "b" = "b" + $2 WHERE "id" = $3`,
			my:   "UPDATE `t` SET `a` = `a` + ?, `b` = `b` + ? WHERE `id` = ?",
			lite: `UPDATE "t" SET "a" = "a" + ?, "b" = "b" + ? WHERE "id" = ?`,
			args: []any{1, 2, 1},
		},
	})
}

func TestDeleteGolden(t *testing.T) {
	runStmts(t, []stmtCase{
		{
			name: "simple delete",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("a", 1).DeleteSQL()
			},
			pg:   `DELETE FROM "t" WHERE "a" = $1`,
			my:   "DELETE FROM `t` WHERE `a` = ?",
			lite: `DELETE FROM "t" WHERE "a" = ?`,
			args: []any{1},
		},
		{
			name: "delete with order and limit",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("a", 1).OrderBy("id").Limit(5).DeleteSQL()
			},
			pg:   `DELETE FROM "t" WHERE "ctid" IN (SELECT "t"."ctid" FROM "t" WHERE "a" = $1 ORDER BY "id" ASC LIMIT 5)`,
			my:   "DELETE FROM `t` WHERE `a` = ? ORDER BY `id` ASC LIMIT 5",
			lite: `DELETE FROM "t" WHERE "rowid" IN (SELECT "t"."rowid" FROM "t" WHERE "a" = ? ORDER BY "id" ASC LIMIT 5)`,
			args: []any{1},
		},
		{
			name: "delete with join",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).
					Table("users as u").
					Join("bans as b", "b.uid", "u.id").
					Where("b.active", true).
					DeleteSQL()
			},
			pg:   `DELETE FROM "users" AS "u" WHERE "ctid" IN (SELECT "u"."ctid" FROM "users" AS "u" INNER JOIN "bans" AS "b" ON "b"."uid" = "u"."id" WHERE "b"."active" = $1)`,
			my:   "DELETE `u` FROM `users` AS `u` INNER JOIN `bans` AS `b` ON `b`.`uid` = `u`.`id` WHERE `b`.`active` = ?",
			lite: `DELETE FROM "users" AS "u" WHERE "rowid" IN (SELECT "u"."rowid" FROM "users" AS "u" INNER JOIN "bans" AS "b" ON "b"."uid" = "u"."id" WHERE "b"."active" = ?)`,
			args: []any{true},
		},
		{
			name: "delete with join and limit",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).
					Table("users as u").
					Join("bans as b", "b.uid", "u.id").
					Where("b.active", true).
					Limit(5).
					DeleteSQL()
			},
			pg:   `DELETE FROM "users" AS "u" WHERE "ctid" IN (SELECT "u"."ctid" FROM "users" AS "u" INNER JOIN "bans" AS "b" ON "b"."uid" = "u"."id" WHERE "b"."active" = $1 LIMIT 5)`,
			my:   unsupported,
			lite: `DELETE FROM "users" AS "u" WHERE "rowid" IN (SELECT "u"."rowid" FROM "users" AS "u" INNER JOIN "bans" AS "b" ON "b"."uid" = "u"."id" WHERE "b"."active" = ? LIMIT 5)`,
			args: []any{true},
		},
		{
			name: "delete returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("a", 1).Returning("id").DeleteSQL()
			},
			pg:   `DELETE FROM "t" WHERE "a" = $1 RETURNING "id"`,
			my:   unsupported,
			lite: `DELETE FROM "t" WHERE "a" = ? RETURNING "id"`,
			args: []any{1},
		},
		{
			name: "delete all rows when confirmed",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").AllRows().DeleteSQL()
			},
			pg:   `DELETE FROM "t"`,
			my:   "DELETE FROM `t`",
			lite: `DELETE FROM "t"`,
		},
		{
			name: "prefix applies to write tables",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").Prefix("p_").Where("id", 1).DeleteSQL()
			},
			pg:   `DELETE FROM "p_users" WHERE "id" = $1`,
			my:   "DELETE FROM `p_users` WHERE `id` = ?",
			lite: `DELETE FROM "p_users" WHERE "id" = ?`,
			args: []any{1},
		},
	})
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		d    db.Dialect
		tbl  string
		want []db.Stmt
	}{
		{db.Postgres, "users", []db.Stmt{{SQL: `TRUNCATE "pre_users" RESTART IDENTITY`}}},
		{db.MySQL, "users", []db.Stmt{{SQL: "TRUNCATE TABLE `pre_users`"}}},
		{db.SQLite, "users", []db.Stmt{
			{SQL: `DELETE FROM sqlite_sequence WHERE name = ?`, Args: []any{"pre_users"}},
			{SQL: `DELETE FROM "pre_users"`},
		}},
		{db.SQLite, "aux.users", []db.Stmt{
			{SQL: `DELETE FROM "aux".sqlite_sequence WHERE name = ?`, Args: []any{"pre_users"}},
			{SQL: `DELETE FROM "aux"."pre_users"`},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.d.String()+"/"+tc.tbl, func(t *testing.T) {
			got, err := db.Use(tc.d).Table(tc.tbl).Prefix("pre_").TruncateSQL()
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestWriteErrors(t *testing.T) {
	d := db.Postgres
	tests := []struct {
		name string
		call func() error
		want error
	}{
		{"no table", func() error { _, _, err := db.NewBuilder(d).InsertSQL(m("a", 1)); return err }, db.ErrNoTable},
		{"no rows", func() error { _, _, err := db.Use(d).Table("t").InsertSQL(); return err }, db.ErrNoValues},
		{
			"ragged rows",
			func() error { _, _, err := db.Use(d).Table("t").InsertSQL(m("a", 1), m("b", 2)); return err },
			db.ErrInvalidArgs,
		},
		{
			"different size rows",
			func() error { _, _, err := db.Use(d).Table("t").InsertSQL(m("a", 1), m("a", 1, "b", 2)); return err },
			db.ErrInvalidArgs,
		},
		{
			"multiple empty rows",
			func() error { _, _, err := db.Use(d).Table("t").InsertSQL(m(), m()); return err },
			db.ErrInvalidArgs,
		},
		{
			"update no values",
			func() error { _, _, err := db.Use(d).Table("t").Where("a", 1).UpdateSQL(nil); return err },
			db.ErrNoValues,
		},
		{
			"update no where",
			func() error { _, _, err := db.Use(d).Table("t").UpdateSQL(m("a", 1)); return err },
			db.ErrNoWhere,
		},
		{"delete no where", func() error { _, _, err := db.Use(d).Table("t").DeleteSQL(); return err }, db.ErrNoWhere},
		{
			"increment no where",
			func() error { _, _, err := db.Use(d).Table("t").IncrementSQL("a", 1, nil); return err },
			db.ErrNoWhere,
		},
		{"increment non numeric", func() error {
			_, _, err := db.Use(d).Table("t").Where("a", 1).IncrementSQL("a", "1; drop", nil)
			return err
		}, db.ErrInvalidArgs},
		{"upsert needs unique", func() error {
			_, _, err := db.Use(d).Table("t").UpsertSQL([]map[string]any{m("a", 1)}, nil, nil)
			return err
		}, db.ErrInvalidArgs},
		{"upsert empty update", func() error {
			_, _, err := db.Use(d).Table("t").UpsertSQL([]map[string]any{m("a", 1)}, []string{"a"}, []string{})
			return err
		}, db.ErrInvalidArgs},
		{
			"insert using nil",
			func() error { _, _, err := db.Use(d).Table("t").InsertUsingSQL(nil, nil); return err },
			db.ErrInvalidArgs,
		},
		{"insert using bad sub", func() error {
			_, _, err := db.Use(d).Table("t").InsertUsingSQL(nil, db.NewBuilder(d))
			return err
		}, db.ErrNoTable},
		{
			"update from no where or join",
			func() error { _, _, err := db.Use(d).Table("t").UpdateFromSQL(m("a", 1)); return err },
			db.ErrNoWhere,
		},
		{"update from sub join", func() error {
			_, _, err := db.Use(d).
				Table("t").
				JoinSub(db.Use(d).Table("s"), "s", "s.id", "t.id").
				UpdateFromSQL(m("a", 1))
			return err
		}, db.ErrUnsupported},
		{
			"builder error surfaces",
			func() error { _, _, err := db.Use(d).Table("t").Where("a", "bogus", 1).DeleteSQL(); return err },
			db.ErrInvalidOperator,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Errorf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestUpdateFromGuards(t *testing.T) {
	if _, _, err := db.Use(db.MySQL).Table("t").UpdateFromSQL(m("a", 1)); !errors.Is(err, db.ErrUnsupported) {
		t.Errorf("mysql UpdateFrom: got %v", err)
	}
	if _, _, err := db.Use(db.Postgres).Table("t").Where("a", 1).UpdateFromSQL(nil); !errors.Is(err, db.ErrNoValues) {
		t.Errorf("no values: got %v", err)
	}
}
