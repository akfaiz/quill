package db_test

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func TestWriteExecutors(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	ok := func(n int64) driver.Result { return sqlmock.NewResult(0, n) }
	sub := func() *db.Builder { return conn.Table("users").Select("name") }

	cases := []struct {
		name string
		sql  string
		args []any
		n    int64
		run  func() (int64, error)
	}{
		{"Insert", `INSERT INTO "users" ("name") VALUES ($1)`, []any{"ann"}, 1,
			func() (int64, error) { return conn.Table("users").Insert(ctx, m("name", "ann")) }},
		{"InsertOrIgnore", `INSERT INTO "users" ("name") VALUES ($1) ON CONFLICT DO NOTHING`, []any{"ann"}, 0,
			func() (int64, error) { return conn.Table("users").InsertOrIgnore(ctx, m("name", "ann")) }},
		{"InsertUsing", `INSERT INTO "archive" ("name") SELECT "name" FROM "users"`, nil, 4,
			func() (int64, error) { return conn.Table("archive").InsertUsing(ctx, []string{"name"}, sub()) }},
		{
			"InsertOrIgnoreUsing",
			`INSERT INTO "archive" ("name") SELECT "name" FROM "users" ON CONFLICT DO NOTHING`,
			nil,
			2,
			func() (int64, error) { return conn.Table("archive").InsertOrIgnoreUsing(ctx, []string{"name"}, sub()) },
		},
		{
			"Upsert",
			`INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"`,
			[]any{"a@x", "ann"},
			1,
			func() (int64, error) {
				return conn.Table("users").Upsert(ctx, []map[string]any{m("email", "a@x", "name", "ann")},
					[]string{"email"}, []string{"name"})
			},
		},
		{"Update", `UPDATE "users" SET "name" = $1 WHERE "id" = $2`, []any{"ann", 7}, 1,
			func() (int64, error) { return conn.Table("users").Where("id", 7).Update(ctx, m("name", "ann")) }},
		{
			"UpdateFrom",
			`UPDATE "posts" SET "hidden" = $1 FROM "users" WHERE "users"."banned" = $2 AND "users"."id" = "posts"."user_id"`,
			[]any{true, true},
			3,
			func() (int64, error) {
				return conn.Table("posts").Join("users", "users.id", "=", "posts.user_id").
					Where("users.banned", true).UpdateFrom(ctx, m("hidden", true))
			},
		},
		{"Increment", `UPDATE "posts" SET "views" = "views" + $1 WHERE "id" = $2`, []any{1, 7}, 1,
			func() (int64, error) { return conn.Table("posts").Where("id", 7).Increment(ctx, "views", 1, nil) }},
		{"Decrement", `UPDATE "posts" SET "views" = "views" - $1 WHERE "id" = $2`, []any{1, 7}, 1,
			func() (int64, error) { return conn.Table("posts").Where("id", 7).Decrement(ctx, "views", 1, nil) }},
		{
			"IncrementEach",
			`UPDATE "stats" SET "clicks" = "clicks" + $1, "views" = "views" + $2 WHERE "day" = $3`,
			[]any{1, 5, "d"},
			1,
			func() (int64, error) {
				return conn.Table("stats").Where("day", "d").IncrementEach(ctx, m("views", 5, "clicks", 1), nil)
			},
		},
		{
			"DecrementEach",
			`UPDATE "stats" SET "clicks" = "clicks" - $1, "views" = "views" - $2 WHERE "day" = $3`,
			[]any{1, 5, "d"},
			1,
			func() (int64, error) {
				return conn.Table("stats").Where("day", "d").DecrementEach(ctx, m("views", 5, "clicks", 1), nil)
			},
		},
		{"Delete", `DELETE FROM "users" WHERE "id" = $1`, []any{7}, 1,
			func() (int64, error) { return conn.Table("users").Where("id", 7).Delete(ctx) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exp := mock.ExpectExec(tc.sql)
			if tc.args != nil {
				exp.WithArgs(argsOf(tc.args)...)
			}
			exp.WillReturnResult(ok(tc.n))
			n, err := tc.run()
			require.NoError(t, err)
			assert.Equal(t, tc.n, n)
		})
	}
}

func argsOf(a []any) []driver.Value {
	out := make([]driver.Value, len(a))
	for i, v := range a {
		out[i] = v
	}
	return out
}

func TestWriteExecutorsPropagateErrors(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	boom := errors.New("boom")

	mock.ExpectExec(`DELETE FROM "users" WHERE "id" = $1`).WithArgs(1).WillReturnError(boom)
	_, err := conn.Table("users").Where("id", 1).Delete(ctx)
	require.ErrorIs(t, err, boom)

	// guards fire before anything is sent
	_, err = conn.Table("users").Delete(ctx)
	require.ErrorIs(t, err, db.ErrNoWhere)
	_, err = conn.Table("users").Update(ctx, m("a", 1))
	require.ErrorIs(t, err, db.ErrNoWhere)

	// nothing to insert is a successful no-op
	n, err := conn.Table("users").Insert(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
	n, err = conn.Table("users").Upsert(ctx, nil, []string{"id"}, nil)
	require.NoError(t, err)
	assert.Zero(t, n)

	_, err = db.Use(db.Postgres).Table("users").Where("id", 1).Delete(ctx)
	require.ErrorIs(t, err, db.ErrNotBound)
}

func TestInsertGetID(t *testing.T) {
	ctx := context.Background()

	t.Run("postgres returns through RETURNING", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(`INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"`).WithArgs("ann").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
		id, err := conn.Table("users").InsertGetID(ctx, m("name", "ann"))
		require.NoError(t, err)
		assert.EqualValues(t, 42, id)
	})
	t.Run("mysql uses LastInsertId", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.MySQL)
		mock.ExpectExec("INSERT INTO `users` (`name`) VALUES (?)").WithArgs("ann").
			WillReturnResult(sqlmock.NewResult(9, 1))
		id, err := conn.Table("users").InsertGetID(ctx, m("name", "ann"))
		require.NoError(t, err)
		assert.EqualValues(t, 9, id)
	})
	t.Run("errors", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		pg := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(`INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"`).WillReturnError(errors.New("boom"))
		_, err := pg.Table("users").InsertGetID(ctx, m("name", "ann"))
		require.Error(t, err)

		my := db.New(sqlDB, db.MySQL)
		mock.ExpectExec("INSERT INTO `users` (`name`) VALUES (?)").WillReturnError(errors.New("boom"))
		_, err = my.Table("users").InsertGetID(ctx, m("name", "ann"))
		require.Error(t, err)

		_, err = pg.Table("users").InsertGetID(ctx, nil)
		require.Error(t, err)
		_, err = db.Use(db.Postgres).Table("users").InsertGetID(ctx, m("a", 1))
		require.ErrorIs(t, err, db.ErrNotBound)
	})
}

func TestUpdateOrInsert(t *testing.T) {
	ctx := context.Background()
	exists := func(v bool) *sqlmock.Rows { return sqlmock.NewRows([]string{"exists"}).AddRow(v) }
	const probe = `SELECT EXISTS(SELECT * FROM "users" WHERE "email" = $1) AS "exists"`

	t.Run("inserts when missing", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(false))
		mock.ExpectExec(`INSERT INTO "users" ("email", "name") VALUES ($1, $2)`).WithArgs("a@x", "ann").
			WillReturnResult(sqlmock.NewResult(1, 1))
		changed, err := conn.Table("users").UpdateOrInsert(ctx, m("email", "a@x"), m("name", "ann"))
		require.NoError(t, err)
		assert.True(t, changed)
	})
	t.Run("updates when present", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(true))
		mock.ExpectExec(`UPDATE "users" SET "name" = $1 WHERE "ctid" IN (SELECT "users"."ctid" FROM "users" WHERE "email" = $2 LIMIT 1)`).
			WithArgs("ann", "a@x").
			WillReturnResult(sqlmock.NewResult(0, 1))
		changed, err := conn.Table("users").UpdateOrInsert(ctx, m("email", "a@x"), m("name", "ann"))
		require.NoError(t, err)
		assert.True(t, changed)
	})
	t.Run("attributes apply to an existing OR as a whole", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		const where = `WHERE ("tenant" = $1 OR "tenant" = $2) AND "email" = $3`
		mock.ExpectQuery(`SELECT EXISTS(SELECT * FROM "users" `+where+`) AS "exists"`).
			WithArgs(1, 2, "a@x").
			WillReturnRows(exists(true))
		mock.ExpectExec(`UPDATE "users" SET "name" = $1 WHERE "ctid" IN (SELECT "users"."ctid" FROM "users" `+
			`WHERE ("tenant" = $2 OR "tenant" = $3) AND "email" = $4 LIMIT 1)`).
			WithArgs("ann", 1, 2, "a@x").
			WillReturnResult(sqlmock.NewResult(0, 1))
		changed, err := conn.Table("users").Where("tenant", 1).OrWhere("tenant", 2).
			UpdateOrInsert(ctx, m("email", "a@x"), m("name", "ann"))
		require.NoError(t, err)
		assert.True(t, changed)
	})
	t.Run("present with no values is a no-op", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(true))
		changed, err := conn.Table("users").UpdateOrInsert(ctx, m("email", "a@x"), nil)
		require.NoError(t, err)
		assert.True(t, changed)
	})
	t.Run("errors", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnError(errors.New("boom"))
		changed, err := conn.Table("users").UpdateOrInsert(ctx, m("email", "a@x"), m("name", "ann"))
		require.Error(t, err)
		assert.False(t, changed)

		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(false))
		mock.ExpectExec(`INSERT INTO "users" ("email", "name") VALUES ($1, $2)`).WillReturnError(errors.New("boom"))
		changed, err = conn.Table("users").UpdateOrInsert(ctx, m("email", "a@x"), m("name", "ann"))
		require.Error(t, err)
		assert.False(t, changed)

		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(true))
		mock.ExpectExec(`UPDATE "users" SET "name" = $1 WHERE "ctid" IN (SELECT "users"."ctid" FROM "users" WHERE "email" = $2 LIMIT 1)`).
			WillReturnError(errors.New("boom"))
		changed, err = conn.Table("users").UpdateOrInsert(ctx, m("email", "a@x"), m("name", "ann"))
		require.Error(t, err)
		assert.False(t, changed)
	})
}

func TestTruncateExecutor(t *testing.T) {
	ctx := context.Background()

	sqlDB, mock := newMock(t)
	mock.ExpectExec(`TRUNCATE "users" RESTART IDENTITY`).WillReturnResult(sqlmock.NewResult(0, 0))
	require.NoError(t, db.New(sqlDB, db.Postgres).Table("users").Truncate(ctx))

	mock.ExpectExec(`TRUNCATE "users" RESTART IDENTITY`).WillReturnError(errors.New("boom"))
	require.Error(t, db.New(sqlDB, db.Postgres).Table("users").Truncate(ctx))

	// SQLite needs two statements, run in one transaction
	lite := db.New(sqlDB, db.SQLite)
	stmts, err := lite.Table("users").TruncateSQL()
	require.NoError(t, err)
	const probe = `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'`
	t.Run("sqlite with sqlite_sequence", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(probe).WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
		for _, s := range stmts {
			mock.ExpectExec(s.SQL).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		mock.ExpectCommit()
		require.NoError(t, lite.Table("users").Truncate(ctx))
	})
	t.Run("sqlite without sqlite_sequence skips the reset", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(probe).WillReturnRows(sqlmock.NewRows([]string{"1"}))
		mock.ExpectExec(stmts[1].SQL).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		require.NoError(t, lite.Table("users").Truncate(ctx))
	})
	t.Run("sqlite rolls back when the delete fails", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(probe).WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
		mock.ExpectExec(stmts[0].SQL).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(stmts[1].SQL).WillReturnError(errors.New("boom"))
		mock.ExpectRollback()
		require.Error(t, lite.Table("users").Truncate(ctx))
	})
	t.Run("sqlite schema-qualified table probes that schema", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT 1 FROM "aux".sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'`).
			WillReturnRows(sqlmock.NewRows([]string{"1"}))
		mock.ExpectExec(`DELETE FROM "aux"."users"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		require.NoError(t, lite.Table("aux.users").Truncate(ctx))
	})

	require.ErrorIs(t, db.Use(db.Postgres).Table("users").Truncate(ctx), db.ErrNotBound)
	require.Error(t, db.New(sqlDB, db.Postgres).Table("users").Where("a", "bogus-op", 1).Truncate(ctx))
}

func TestReturningWrites(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`INSERT INTO "users" ("name") VALUES ($1) RETURNING "id", "name"`).WithArgs("ann").
		WillReturnRows(people(1))
	var out []person
	require.NoError(t, conn.Table("users").Returning("id", "name").InsertReturning(ctx, &out, m("name", "ann")))
	assert.Equal(t, []person{{1, "p1"}}, out)

	mock.ExpectQuery(`UPDATE "users" SET "name" = $1 WHERE "id" = $2 RETURNING "id", "name"`).WithArgs("ann", 1).
		WillReturnRows(people(1))
	require.NoError(
		t,
		conn.Table("users").Where("id", 1).Returning("id", "name").UpdateReturning(ctx, &out, m("name", "ann")),
	)

	mock.ExpectQuery(`DELETE FROM "users" WHERE "id" = $1 RETURNING "id", "name"`).WithArgs(1).
		WillReturnRows(people(1))
	require.NoError(t, conn.Table("users").Where("id", 1).Returning("id", "name").DeleteReturning(ctx, &out))

	// Returning(...) is mandatory
	require.ErrorIs(t, conn.Table("users").InsertReturning(ctx, &out, m("name", "ann")), db.ErrInvalidArgs)
	require.ErrorIs(
		t,
		db.Use(db.Postgres).Table("users").Returning("id").InsertReturning(ctx, &out, m("a", 1)),
		db.ErrNotBound,
	)
	require.Error(t, conn.Table("users").Returning("id").InsertReturning(ctx, &out))
}
