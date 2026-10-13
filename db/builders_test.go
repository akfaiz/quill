package db_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func hasMethod(v any, name string) bool {
	_, ok := reflect.TypeOf(v).MethodByName(name)
	return ok
}

// TestBuilderMethodSets pins which statement owns which methods, so a clause that does
// not apply to a statement cannot be added to its builder by accident.
func TestBuilderMethodSets(t *testing.T) {
	var (
		sel *db.SelectBuilder
		ins *db.InsertBuilder
		upd *db.UpdateBuilder
		del *db.DeleteBuilder
	)
	cases := []struct {
		name    string
		builder any
		absent  []string
		present []string
	}{
		{
			"select",
			sel,
			[]string{"Set", "Values", "Exec", "Returning", "AllRows"},
			[]string{"Where", "Join", "GroupBy", "Having", "Union", "Get"},
		},
		{
			"insert",
			ins,
			[]string{"Where", "WhereIn", "Join", "OrderBy", "Limit", "GroupBy", "Set"},
			[]string{"Values", "Rows", "Returning", "Exec", "Upsert"},
		},
		{
			"update",
			upd,
			[]string{"Values", "Rows", "GroupBy", "Having", "Union", "Offset", "Select"},
			[]string{"Where", "Join", "Limit", "Returning", "AllRows", "Set"},
		},
		{
			"delete",
			del,
			[]string{"Set", "Values", "GroupBy", "Having", "Union", "Offset", "Select"},
			[]string{"Where", "Returning", "AllRows"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, m := range tc.absent {
				assert.False(t, hasMethod(tc.builder, m), "%s builder must not have %s", tc.name, m)
			}
			for _, m := range tc.present {
				assert.True(t, hasMethod(tc.builder, m), "%s builder must have %s", tc.name, m)
			}
		})
	}
}

type tagged struct {
	ID   int64  `db:"id,omitempty"`
	Name string `db:"name"`
}

func TestWriteBuilderSemantics(t *testing.T) {
	cases := []struct {
		name string
		run  func(c *db.DB) (string, []any, error)
		sql  map[db.Dialect]string
		args []any
	}{
		{
			name: "update set twice keeps the last value in the first position",
			run: func(c *db.DB) (string, []any, error) {
				return c.Update("t").Set("a", 1).Set("b", 2).Set("a", 3).Where("id", 9).ToSQL()
			},
			sql: map[db.Dialect]string{
				db.Postgres: `UPDATE "t" SET "a" = $1, "b" = $2 WHERE "id" = $3`,
				db.MySQL:    "UPDATE `t` SET `a` = ?, `b` = ? WHERE `id` = ?",
				db.SQLite:   `UPDATE "t" SET "a" = ?, "b" = ? WHERE "id" = ?`,
			},
			args: []any{3, 2, 9},
		},
		{
			name: "insert rows omits an omitempty column that is zero in every row",
			run: func(c *db.DB) (string, []any, error) {
				return c.Insert("t").Rows(tagged{Name: "a"}, tagged{Name: "b"}).ToSQL()
			},
			sql: map[db.Dialect]string{
				db.Postgres: `INSERT INTO "t" ("name") VALUES ($1), ($2)`,
				db.MySQL:    "INSERT INTO `t` (`name`) VALUES (?), (?)",
				db.SQLite:   `INSERT INTO "t" ("name") VALUES (?), (?)`,
			},
			args: []any{"a", "b"},
		},
		{
			name: "insert rows keeps an omitempty column set in any row",
			run: func(c *db.DB) (string, []any, error) {
				return c.Insert("t").Rows(tagged{Name: "a"}, tagged{ID: 2, Name: "b"}).ToSQL()
			},
			sql: map[db.Dialect]string{
				db.Postgres: `INSERT INTO "t" ("id", "name") VALUES ($1, $2), ($3, $4)`,
				db.MySQL:    "INSERT INTO `t` (`id`, `name`) VALUES (?, ?), (?, ?)",
				db.SQLite:   `INSERT INTO "t" ("id", "name") VALUES (?, ?), (?, ?)`,
			},
			args: []any{int64(0), "a", int64(2), "b"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for d, want := range tc.sql {
				sql, args, err := tc.run(db.Use(d))
				require.NoError(t, err, d)
				assert.Equal(t, want, sql, d)
				assert.Equal(t, tc.args, args, d)
			}
		})
	}
}

func TestWriteBuilderErrors(t *testing.T) {
	conn := db.Use(db.Postgres)
	sub := func() *db.SelectBuilder { return conn.Select("a").From("s") }
	var nilSub *db.SelectBuilder

	_, _, err := conn.Update("t").Increment("n", "many").Where("id", 1).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)

	_, _, err = conn.Insert("t").Using([]string{"a"}, sub()).Upsert([]string{"a"}, nil).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)

	_, _, err = conn.Insert("t").Using([]string{"a"}, nilSub).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)
	require.ErrorContains(t, err, "nil subquery")

	_, _, err = conn.Select().From("t").WhereExists(nilSub).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)
	require.ErrorContains(t, err, "nil subquery")
}

// TestRowidSubqueryHoldsOnlyRowSelection pins the ctid/rowid subquery used when a
// write has a Join plus Limit (or an ORDER BY): it must carry the table, joins, wheres,
// order and limit, and nothing from the outer statement such as the SET values.
func TestRowidSubqueryHoldsOnlyRowSelection(t *testing.T) {
	pg, lite := db.Use(db.Postgres), db.Use(db.SQLite)

	sql, args, err := pg.Update("posts").Join("users", "users.id", "=", "posts.user_id").
		Where("users.banned", true).OrderBy("posts.id").Limit(1).Set("hidden", true).ToSQL()
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "posts" SET "hidden" = $1 WHERE "ctid" IN (SELECT "posts"."ctid" FROM "posts" `+
		`INNER JOIN "users" ON "users"."id" = "posts"."user_id" WHERE "users"."banned" = $2 `+
		`ORDER BY "posts"."id" ASC LIMIT 1)`, sql)
	assert.Equal(t, []any{true, true}, args)

	sql, args, err = lite.Update("posts").Join("users", "users.id", "=", "posts.user_id").
		Where("users.banned", true).OrderBy("posts.id").Limit(1).Set("hidden", true).ToSQL()
	require.NoError(t, err)
	assert.Equal(t, `UPDATE "posts" SET "hidden" = ? WHERE "rowid" IN (SELECT "posts"."rowid" FROM "posts" `+
		`INNER JOIN "users" ON "users"."id" = "posts"."user_id" WHERE "users"."banned" = ? `+
		`ORDER BY "posts"."id" ASC LIMIT 1)`, sql)
	assert.Equal(t, []any{true, true}, args)

	sql, args, err = pg.Delete("t").Where("a", 1).OrderBy("id").Limit(5).ToSQL()
	require.NoError(t, err)
	assert.Equal(
		t,
		`DELETE FROM "t" WHERE "ctid" IN (SELECT "t"."ctid" FROM "t" WHERE "a" = $1 ORDER BY "id" ASC LIMIT 5)`,
		sql,
	)
	assert.Equal(t, []any{1}, args)
}
