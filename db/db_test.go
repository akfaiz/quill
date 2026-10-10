package db_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func TestUseSetsTheDialectThenChainsFromTable(t *testing.T) {
	tests := []struct {
		d    db.Dialect
		want string
	}{
		{db.Postgres, `SELECT "id", "name" FROM "users" WHERE "votes" > $1 ORDER BY "id" DESC LIMIT 10`},
		{db.MySQL, "SELECT `id`, `name` FROM `users` WHERE `votes` > ? ORDER BY `id` DESC LIMIT 10"},
		{db.SQLite, `SELECT "id", "name" FROM "users" WHERE "votes" > ? ORDER BY "id" DESC LIMIT 10`},
		{db.MariaDB, "SELECT `id`, `name` FROM `users` WHERE `votes` > ? ORDER BY `id` DESC LIMIT 10"},
	}
	for _, tc := range tests {
		t.Run(tc.d.String(), func(t *testing.T) {
			sql, args, err := db.Use(tc.d).Table("users").
				Select("id", "name").
				Where("votes", ">", 100).
				OrderByDesc("id").
				Limit(10).
				ToSQL()
			require.NoError(t, err)
			if sql != tc.want || !reflect.DeepEqual(args, []any{100}) {
				t.Errorf("got  %s %v\nwant %s [100]", sql, args, tc.want)
			}
		})
	}
}

func TestUseIsReusableAndKeepsBuildersIndependent(t *testing.T) {
	pg := db.Use(db.Postgres) // set once, chain many times
	a, _, _ := pg.Table("a").Where("x", 1).ToSQL()
	b, _, _ := pg.Table("b").Limit(1).ToSQL()
	if a != `SELECT * FROM "a" WHERE "x" = $1` || b != `SELECT * FROM "b" LIMIT 1` {
		t.Errorf("builders leaked into each other:\n a: %s\n b: %s", a, b)
	}
	// NewQuery gives a table-less builder for subquery sources
	sql, _, err := pg.NewQuery().FromSub(pg.Table("inner").Where("k", 1), "i").ToSQL()
	if err != nil || sql != `SELECT * FROM (SELECT * FROM "inner" WHERE "k" = $1) AS "i"` {
		t.Errorf("got %s (%v)", sql, err)
	}
}

func TestUseAppliesOptions(t *testing.T) {
	sql, _, err := db.Use(db.MySQL, db.WithPrefix("app_")).Table("users as u").Where("u.id", 1).ToSQL()
	require.NoError(t, err)
	if want := "SELECT * FROM `app_users` AS `app_u` WHERE `app_u`.`id` = ?"; sql != want {
		t.Errorf("got %s\nwant %s", sql, want)
	}
}

func TestUseWithAnUnknownDialectFailsAtTheTerminalCall(t *testing.T) {
	q := db.Use(db.Dialect(99)).Table("users").Where("id", 1) // the chain itself keeps working
	if _, _, err := q.ToSQL(); !errors.Is(err, db.ErrUnknownDialect) {
		t.Errorf("got %v, want ErrUnknownDialect", err)
	}
}

func TestUseCannotRunQueriesButNeverPanics(t *testing.T) {
	ctx := context.Background()
	conn := db.Use(db.Postgres)
	users := func() *db.Builder { return conn.Table("users") }
	var rows []map[string]any
	var one map[string]any
	var n int64

	calls := map[string]func() error{
		"Get":         func() error { return users().Get(ctx, &rows) },
		"First":       func() error { return users().First(ctx, &one) },
		"Find":        func() error { return users().Find(ctx, 1, &one) },
		"Count":       func() error { _, err := users().Count(ctx); return err },
		"Exists":      func() error { _, err := users().Exists(ctx); return err },
		"Aggregate":   func() error { return users().Aggregate(ctx, "sum", "x", &n) },
		"Paginate":    func() error { _, err := users().Paginate(ctx, 1, 10, &rows); return err },
		"Each":        func() error { return users().Each(ctx, &one, func() error { return nil }) },
		"Insert":      func() error { _, err := users().Insert(ctx, map[string]any{"a": 1}); return err },
		"InsertGetID": func() error { _, err := users().InsertGetID(ctx, map[string]any{"a": 1}); return err },
		"Update":      func() error { _, err := users().Where("id", 1).Update(ctx, map[string]any{"a": 1}); return err },
		"Delete":      func() error { _, err := users().Where("id", 1).Delete(ctx); return err },
		"Truncate":    func() error { return users().Truncate(ctx) },
		"DB.Query":    func() error { return conn.Query(ctx, &rows, "SELECT 1") },
		"DB.Exec":     func() error { _, err := conn.Exec(ctx, "SELECT 1"); return err },
		"Transaction": func() error { return conn.Transaction(ctx, func(*db.DB) error { return nil }) },
		"typed Get":   func() error { _, err := db.From[chainRow](conn).Get(ctx); return err },
		"typed Insert": func() error {
			_, err := db.From[chainRow](conn).Insert(ctx, chainRow{ID: 1})
			return err
		},
		"Stream": func() error {
			var err error
			for _, e := range db.Stream[map[string]any](ctx, users()) {
				err = e
			}
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, db.ErrNotBound) {
				t.Errorf("got %v, want ErrNotBound", err)
			}
		})
	}
}
