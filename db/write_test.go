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
			pg:   `insert into "users" ("age", "name") values ($1, $2)`,
			my:   "insert into `users` (`age`, `name`) values (?, ?)",
			lite: `insert into "users" ("age", "name") values (?, ?)`,
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
			pg:   `insert into "users" ("a", "b") values ($1, now()), ($2, (select "id" from "teams" where "slug" = $3))`,
			my:   "insert into `users` (`a`, `b`) values (?, now()), (?, (select `id` from `teams` where `slug` = ?))",
			lite: `insert into "users" ("a", "b") values (?, now()), (?, (select "id" from "teams" where "slug" = ?))`,
			args: []any{1, 2, "x"},
		},
		{
			name: "default values",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertSQL(m())
			},
			pg:   `insert into "t" default values`,
			my:   "insert into `t` () values ()",
			lite: `insert into "t" default values`,
		},
		{
			name: "insert or ignore",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertOrIgnoreSQL(m("a", 1))
			},
			pg:   `insert into "t" ("a") values ($1) on conflict do nothing`,
			my:   "insert ignore into `t` (`a`) values (?)",
			lite: `insert or ignore into "t" ("a") values (?)`,
			args: []any{1},
		},
		{
			name: "insert get id",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertGetIDSQL(m("a", 1))
			},
			pg:   `insert into "t" ("a") values ($1) returning "id"`,
			my:   "insert into `t` (`a`) values (?)",
			lite: `insert into "t" ("a") values (?)`,
			args: []any{1},
		},
		{
			name: "insert get id custom sequence",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").InsertGetIDSQL(m("a", 1), "uid")
			},
			pg:   `insert into "t" ("a") values ($1) returning "uid"`,
			my:   "insert into `t` (`a`) values (?)",
			lite: `insert into "t" ("a") values (?)`,
			args: []any{1},
		},
		{
			name: "returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Returning("id", "name").InsertSQL(m("a", 1))
			},
			pg:   `insert into "t" ("a") values ($1) returning "id", "name"`,
			my:   unsupported,
			lite: `insert into "t" ("a") values (?) returning "id", "name"`,
			args: []any{1},
		},
		{
			name: "insert using",
			build: func(d db.Dialect) (string, []any, error) {
				src := db.Use(d).Table("old").Select("a", "b").Where("n", ">", 1)
				return db.Use(d).Table("new").InsertUsingSQL([]string{"a", "b"}, src)
			},
			pg:   `insert into "new" ("a", "b") select "a", "b" from "old" where "n" > $1`,
			my:   "insert into `new` (`a`, `b`) select `a`, `b` from `old` where `n` > ?",
			lite: `insert into "new" ("a", "b") select "a", "b" from "old" where "n" > ?`,
			args: []any{1},
		},
		{
			name: "insert or ignore using without column list",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("new").InsertOrIgnoreUsingSQL([]string{"*"}, db.Use(d).Table("old"))
			},
			pg:   `insert into "new" select * from "old" on conflict do nothing`,
			my:   "insert ignore into `new` select * from `old`",
			lite: `insert or ignore into "new" select * from "old"`,
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
			pg:   `insert into "users" ("email", "name") values ($1, $2), ($3, $4) on conflict ("email") do update set "name" = "excluded"."name"`,
			my:   "insert into `users` (`email`, `name`) values (?, ?), (?, ?) on duplicate key update `name` = values(`name`)",
			lite: `insert into "users" ("email", "name") values (?, ?), (?, ?) on conflict ("email") do update set "name" = "excluded"."name"`,
			args: []any{"a@x", "A", "b@x", "B"},
		},
		{
			name: "nil update means all columns, returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").Returning("id").UpsertSQL(rows[:1], []string{"email"}, nil)
			},
			pg:   `insert into "users" ("email", "name") values ($1, $2) on conflict ("email") do update set "email" = "excluded"."email", "name" = "excluded"."name" returning "id"`,
			my:   unsupported,
			lite: `insert into "users" ("email", "name") values (?, ?) on conflict ("email") do update set "email" = "excluded"."email", "name" = "excluded"."name" returning "id"`,
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
			pg:   `update "users" set "age" = $1, "name" = $2 where "id" = $3`,
			my:   "update `users` set `age` = ?, `name` = ? where `id` = ?",
			lite: `update "users" set "age" = ?, "name" = ? where "id" = ?`,
			args: []any{5, "z", 7},
		},
		{
			name: "raw expression and returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("id", 1).Returning("*").UpdateSQL(m("n", db.Raw("n + ?", 2)))
			},
			pg:   `update "t" set "n" = n + $1 where "id" = $2 returning *`,
			my:   unsupported,
			lite: `update "t" set "n" = n + ? where "id" = ? returning *`,
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
			pg:   `update "users" as "u" set "vip" = $1 where "ctid" in (select "u"."ctid" from "users" as "u" inner join "orders" as "o" on "o"."uid" = "u"."id" where "o"."total" > $2)`,
			my:   "update `users` as `u` inner join `orders` as `o` on `o`.`uid` = `u`.`id` set `u`.`vip` = ? where `o`.`total` > ?",
			lite: `update "users" as "u" set "vip" = ? where "rowid" in (select "u"."rowid" from "users" as "u" inner join "orders" as "o" on "o"."uid" = "u"."id" where "o"."total" > ?)`,
			args: []any{true, 100},
		},
		{
			name: "update with limit",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("a", 1).OrderBy("id").Limit(2).UpdateSQL(m("b", 2))
			},
			pg:   `update "t" set "b" = $1 where "ctid" in (select "t"."ctid" from "t" where "a" = $2 order by "id" asc limit 2)`,
			my:   "update `t` set `b` = ? where `a` = ? order by `id` asc limit 2",
			lite: `update "t" set "b" = ? where "rowid" in (select "t"."rowid" from "t" where "a" = ? order by "id" asc limit 2)`,
			args: []any{2, 1},
		},
		{
			name: "update all rows when confirmed",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").AllRows().UpdateSQL(m("a", 1))
			},
			pg:   `update "t" set "a" = $1`,
			my:   "update `t` set `a` = ?",
			lite: `update "t" set "a" = ?`,
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
			pg:   `update "users" set "plan" = "accounts"."plan" from "accounts" where "accounts"."active" = $1 and "accounts"."uid" = "users"."id"`,
			my:   unsupported,
			lite: unsupported,
			args: []any{true},
		},
		{
			name: "update from without base where uses join conditions",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").Join("a", "a.uid", "users.id").UpdateFromSQL(m("x", 1))
			},
			pg:   `update "users" set "x" = $1 from "a" where "a"."uid" = "users"."id"`,
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
			pg:   `update "posts" set "views" = "views" + $1, "seen_at" = $2 where "id" = $3`,
			my:   "update `posts` set `views` = `views` + ?, `seen_at` = ? where `id` = ?",
			lite: `update "posts" set "views" = "views" + ?, "seen_at" = ? where "id" = ?`,
			args: []any{1, "now", 3},
		},
		{
			name: "decrement each with override",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("stock").Where("sku", "a").DecrementEachSQL(m("qty", 2, "reserved", 1.5), m("reserved", 0))
			},
			pg:   `update "stock" set "qty" = "qty" - $1, "reserved" = $2 where "sku" = $3`,
			my:   "update `stock` set `qty` = `qty` - ?, `reserved` = ? where `sku` = ?",
			lite: `update "stock" set "qty" = "qty" - ?, "reserved" = ? where "sku" = ?`,
			args: []any{2, 0, "a"},
		},
		{
			name: "qualified column in increment (mysql join target)",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("id", 1).IncrementEachSQL(m("a", 1, "b", 2), nil)
			},
			pg:   `update "t" set "a" = "a" + $1, "b" = "b" + $2 where "id" = $3`,
			my:   "update `t` set `a` = `a` + ?, `b` = `b` + ? where `id` = ?",
			lite: `update "t" set "a" = "a" + ?, "b" = "b" + ? where "id" = ?`,
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
			pg:   `delete from "t" where "a" = $1`,
			my:   "delete from `t` where `a` = ?",
			lite: `delete from "t" where "a" = ?`,
			args: []any{1},
		},
		{
			name: "delete with order and limit",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("a", 1).OrderBy("id").Limit(5).DeleteSQL()
			},
			pg:   `delete from "t" where "ctid" in (select "t"."ctid" from "t" where "a" = $1 order by "id" asc limit 5)`,
			my:   "delete from `t` where `a` = ? order by `id` asc limit 5",
			lite: `delete from "t" where "rowid" in (select "t"."rowid" from "t" where "a" = ? order by "id" asc limit 5)`,
			args: []any{1},
		},
		{
			name: "delete with join",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users as u").Join("bans as b", "b.uid", "u.id").Where("b.active", true).DeleteSQL()
			},
			pg:   `delete from "users" as "u" where "ctid" in (select "u"."ctid" from "users" as "u" inner join "bans" as "b" on "b"."uid" = "u"."id" where "b"."active" = $1)`,
			my:   "delete `u` from `users` as `u` inner join `bans` as `b` on `b`.`uid` = `u`.`id` where `b`.`active` = ?",
			lite: `delete from "users" as "u" where "rowid" in (select "u"."rowid" from "users" as "u" inner join "bans" as "b" on "b"."uid" = "u"."id" where "b"."active" = ?)`,
			args: []any{true},
		},
		{
			name: "delete returning",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").Where("a", 1).Returning("id").DeleteSQL()
			},
			pg:   `delete from "t" where "a" = $1 returning "id"`,
			my:   unsupported,
			lite: `delete from "t" where "a" = ? returning "id"`,
			args: []any{1},
		},
		{
			name: "delete all rows when confirmed",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("t").AllRows().DeleteSQL()
			},
			pg:   `delete from "t"`,
			my:   "delete from `t`",
			lite: `delete from "t"`,
		},
		{
			name: "prefix applies to write tables",
			build: func(d db.Dialect) (string, []any, error) {
				return db.Use(d).Table("users").Prefix("p_").Where("id", 1).DeleteSQL()
			},
			pg:   `delete from "p_users" where "id" = $1`,
			my:   "delete from `p_users` where `id` = ?",
			lite: `delete from "p_users" where "id" = ?`,
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
		{db.Postgres, "users", []db.Stmt{{SQL: `truncate "pre_users" restart identity`}}},
		{db.MySQL, "users", []db.Stmt{{SQL: "truncate table `pre_users`"}}},
		{db.SQLite, "users", []db.Stmt{
			{SQL: `delete from sqlite_sequence where name = ?`, Args: []any{"pre_users"}},
			{SQL: `delete from "pre_users"`},
		}},
		{db.SQLite, "aux.users", []db.Stmt{
			{SQL: `delete from "aux".sqlite_sequence where name = ?`, Args: []any{"pre_users"}},
			{SQL: `delete from "aux"."pre_users"`},
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
		{"ragged rows", func() error { _, _, err := db.Use(d).Table("t").InsertSQL(m("a", 1), m("b", 2)); return err }, db.ErrInvalidArgs},
		{"different size rows", func() error { _, _, err := db.Use(d).Table("t").InsertSQL(m("a", 1), m("a", 1, "b", 2)); return err }, db.ErrInvalidArgs},
		{"multiple empty rows", func() error { _, _, err := db.Use(d).Table("t").InsertSQL(m(), m()); return err }, db.ErrInvalidArgs},
		{"update no values", func() error { _, _, err := db.Use(d).Table("t").Where("a", 1).UpdateSQL(nil); return err }, db.ErrNoValues},
		{"update no where", func() error { _, _, err := db.Use(d).Table("t").UpdateSQL(m("a", 1)); return err }, db.ErrNoWhere},
		{"delete no where", func() error { _, _, err := db.Use(d).Table("t").DeleteSQL(); return err }, db.ErrNoWhere},
		{"increment no where", func() error { _, _, err := db.Use(d).Table("t").IncrementSQL("a", 1, nil); return err }, db.ErrNoWhere},
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
		{"insert using nil", func() error { _, _, err := db.Use(d).Table("t").InsertUsingSQL(nil, nil); return err }, db.ErrInvalidArgs},
		{"insert using bad sub", func() error {
			_, _, err := db.Use(d).Table("t").InsertUsingSQL(nil, db.NewBuilder(d))
			return err
		}, db.ErrNoTable},
		{"update from no where or join", func() error { _, _, err := db.Use(d).Table("t").UpdateFromSQL(m("a", 1)); return err }, db.ErrNoWhere},
		{"update from sub join", func() error {
			_, _, err := db.Use(d).Table("t").JoinSub(db.Use(d).Table("s"), "s", "s.id", "t.id").UpdateFromSQL(m("a", 1))
			return err
		}, db.ErrUnsupported},
		{"builder error surfaces", func() error { _, _, err := db.Use(d).Table("t").Where("a", "bogus", 1).DeleteSQL(); return err }, db.ErrInvalidOperator},
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
