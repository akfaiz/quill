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
	sub := func() *db.SelectBuilder { return conn.Select("name").From("users") }

	cases := []struct {
		name string
		sql  string
		args []any
		n    int64
		run  func() (int64, error)
	}{
		{"Insert", `INSERT INTO "users" ("name") VALUES ($1)`, []any{"ann"}, 1,
			func() (int64, error) { return conn.Insert("users").Values(m("name", "ann")).Exec(ctx) }},
		{"InsertOrIgnore", `INSERT INTO "users" ("name") VALUES ($1) ON CONFLICT DO NOTHING`, []any{"ann"}, 0,
			func() (int64, error) { return conn.Insert("users").Values(m("name", "ann")).Ignore().Exec(ctx) }},
		{"InsertUsing", `INSERT INTO "archive" ("name") SELECT "name" FROM "users"`, nil, 4,
			func() (int64, error) { return conn.Insert("archive").Using([]string{"name"}, sub()).Exec(ctx) }},
		{
			"InsertOrIgnoreUsing",
			`INSERT INTO "archive" ("name") SELECT "name" FROM "users" ON CONFLICT DO NOTHING`,
			nil,
			2,
			func() (int64, error) { return conn.Insert("archive").Using([]string{"name"}, sub()).Ignore().Exec(ctx) },
		},
		{
			"Upsert",
			`INSERT INTO "users" ("email", "name") VALUES ($1, $2) ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"`,
			[]any{"a@x", "ann"},
			1,
			func() (int64, error) {
				return conn.Insert("users").Values(m("email", "a@x", "name", "ann")).
					Upsert([]string{"email"}, []string{"name"}).Exec(ctx)
			},
		},
		{"Update", `UPDATE "users" SET "name" = $1 WHERE "id" = $2`, []any{"ann", 7}, 1,
			func() (int64, error) { return conn.Update("users").SetMap(m("name", "ann")).Where("id", 7).Exec(ctx) }},
		{
			"UpdateFrom",
			`UPDATE "posts" SET "hidden" = $1 FROM "users" WHERE "users"."banned" = $2 AND "users"."id" = "posts"."user_id"`,
			[]any{true, true},
			3,
			func() (int64, error) {
				return conn.Update("posts").Join("users", "users.id", "=", "posts.user_id").FromJoins().
					Where("users.banned", true).SetMap(m("hidden", true)).Exec(ctx)
			},
		},
		{"Increment", `UPDATE "posts" SET "views" = "views" + $1 WHERE "id" = $2`, []any{1, 7}, 1,
			func() (int64, error) { return conn.Update("posts").Increment("views", 1).Where("id", 7).Exec(ctx) }},
		{"Decrement", `UPDATE "posts" SET "views" = "views" - $1 WHERE "id" = $2`, []any{1, 7}, 1,
			func() (int64, error) { return conn.Update("posts").Decrement("views", 1).Where("id", 7).Exec(ctx) }},
		{
			"IncrementEach",
			`UPDATE "stats" SET "clicks" = "clicks" + $1, "views" = "views" + $2 WHERE "day" = $3`,
			[]any{1, 5, "d"},
			1,
			func() (int64, error) {
				return conn.Update("stats").IncrementEach(m("views", 5, "clicks", 1)).Where("day", "d").Exec(ctx)
			},
		},
		{
			"DecrementEach",
			`UPDATE "stats" SET "clicks" = "clicks" - $1, "views" = "views" - $2 WHERE "day" = $3`,
			[]any{1, 5, "d"},
			1,
			func() (int64, error) {
				return conn.Update("stats").DecrementEach(m("views", 5, "clicks", 1)).Where("day", "d").Exec(ctx)
			},
		},
		{"Delete", `DELETE FROM "users" WHERE "id" = $1`, []any{7}, 1,
			func() (int64, error) { return conn.Delete("users").Where("id", 7).Exec(ctx) }},
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
	_, err := conn.Delete("users").Where("id", 1).Exec(ctx)
	require.ErrorIs(t, err, boom)

	// guards fire before anything is sent
	_, err = conn.Delete("users").Exec(ctx)
	require.ErrorIs(t, err, db.ErrNoWhere)
	_, err = conn.Update("users").Set("a", 1).Exec(ctx)
	require.ErrorIs(t, err, db.ErrNoWhere)

	// nothing to insert is a successful no-op
	n, err := conn.Insert("users").Exec(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)
	n, err = conn.Insert("users").Upsert([]string{"id"}, nil).Exec(ctx)
	require.NoError(t, err)
	assert.Zero(t, n)

	_, err = db.Use(db.Postgres).Delete("users").Where("id", 1).Exec(ctx)
	require.ErrorIs(t, err, db.ErrNotBound)
}

func TestInsertGetID(t *testing.T) {
	ctx := context.Background()

	t.Run("postgres returns through RETURNING", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(`INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"`).WithArgs("ann").
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
		id, err := conn.Insert("users").Values(m("name", "ann")).ExecGetID(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 42, id)
	})
	t.Run("mysql uses LastInsertId", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.MySQL)
		mock.ExpectExec("INSERT INTO `users` (`name`) VALUES (?)").WithArgs("ann").
			WillReturnResult(sqlmock.NewResult(9, 1))
		id, err := conn.Insert("users").Values(m("name", "ann")).ExecGetID(ctx)
		require.NoError(t, err)
		assert.EqualValues(t, 9, id)
	})
	t.Run("errors", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		pg := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(`INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"`).WillReturnError(errors.New("boom"))
		_, err := pg.Insert("users").Values(m("name", "ann")).ExecGetID(ctx)
		require.Error(t, err)

		my := db.New(sqlDB, db.MySQL)
		mock.ExpectExec("INSERT INTO `users` (`name`) VALUES (?)").WillReturnError(errors.New("boom"))
		_, err = my.Insert("users").Values(m("name", "ann")).ExecGetID(ctx)
		require.Error(t, err)

		_, err = pg.Insert("users").ExecGetID(ctx)
		require.Error(t, err)
		_, err = db.Use(db.Postgres).Insert("users").Values(m("a", 1)).ExecGetID(ctx)
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
		changed, err := conn.UpdateOrInsert(ctx, "users", m("email", "a@x"), m("name", "ann"))
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
		changed, err := conn.UpdateOrInsert(ctx, "users", m("email", "a@x"), m("name", "ann"))
		require.NoError(t, err)
		assert.True(t, changed)
	})
	t.Run("present with no values is a no-op", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(true))
		changed, err := conn.UpdateOrInsert(ctx, "users", m("email", "a@x"), nil)
		require.NoError(t, err)
		assert.True(t, changed)
	})
	t.Run("errors", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		conn := db.New(sqlDB, db.Postgres)
		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnError(errors.New("boom"))
		changed, err := conn.UpdateOrInsert(ctx, "users", m("email", "a@x"), m("name", "ann"))
		require.Error(t, err)
		assert.False(t, changed)

		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(false))
		mock.ExpectExec(`INSERT INTO "users" ("email", "name") VALUES ($1, $2)`).WillReturnError(errors.New("boom"))
		changed, err = conn.UpdateOrInsert(ctx, "users", m("email", "a@x"), m("name", "ann"))
		require.Error(t, err)
		assert.False(t, changed)

		mock.ExpectQuery(probe).WithArgs("a@x").WillReturnRows(exists(true))
		mock.ExpectExec(`UPDATE "users" SET "name" = $1 WHERE "ctid" IN (SELECT "users"."ctid" FROM "users" WHERE "email" = $2 LIMIT 1)`).
			WillReturnError(errors.New("boom"))
		changed, err = conn.UpdateOrInsert(ctx, "users", m("email", "a@x"), m("name", "ann"))
		require.Error(t, err)
		assert.False(t, changed)
	})
}

func TestTruncateExecutor(t *testing.T) {
	ctx := context.Background()

	sqlDB, mock := newMock(t)
	mock.ExpectExec(`TRUNCATE "users" RESTART IDENTITY`).WillReturnResult(sqlmock.NewResult(0, 0))
	require.NoError(t, db.New(sqlDB, db.Postgres).Truncate(ctx, "users"))

	mock.ExpectExec(`TRUNCATE "users" RESTART IDENTITY`).WillReturnError(errors.New("boom"))
	require.Error(t, db.New(sqlDB, db.Postgres).Truncate(ctx, "users"))

	// SQLite needs two statements, run in one transaction
	lite := db.New(sqlDB, db.SQLite)
	stmts, err := lite.TruncateSQL("users")
	require.NoError(t, err)
	const probe = `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'`
	t.Run("sqlite with sqlite_sequence", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(probe).WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
		for _, s := range stmts {
			mock.ExpectExec(s.SQL).WillReturnResult(sqlmock.NewResult(0, 0))
		}
		mock.ExpectCommit()
		require.NoError(t, lite.Truncate(ctx, "users"))
	})
	t.Run("sqlite without sqlite_sequence skips the reset", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(probe).WillReturnRows(sqlmock.NewRows([]string{"1"}))
		mock.ExpectExec(stmts[1].SQL).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		require.NoError(t, lite.Truncate(ctx, "users"))
	})
	t.Run("sqlite rolls back when the delete fails", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(probe).WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
		mock.ExpectExec(stmts[0].SQL).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec(stmts[1].SQL).WillReturnError(errors.New("boom"))
		mock.ExpectRollback()
		require.Error(t, lite.Truncate(ctx, "users"))
	})
	t.Run("sqlite schema-qualified table probes that schema", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT 1 FROM "aux".sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'`).
			WillReturnRows(sqlmock.NewRows([]string{"1"}))
		mock.ExpectExec(`DELETE FROM "aux"."users"`).WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()
		require.NoError(t, lite.Truncate(ctx, "aux.users"))
	})

	require.ErrorIs(t, db.Use(db.Postgres).Truncate(ctx, "users"), db.ErrNotBound)
}

func TestReturningWrites(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`INSERT INTO "users" ("name") VALUES ($1) RETURNING "id", "name"`).WithArgs("ann").
		WillReturnRows(people(1))
	var out []person
	require.NoError(t, conn.Insert("users").Values(m("name", "ann")).Returning("id", "name").Get(ctx, &out))
	assert.Equal(t, []person{{1, "p1"}}, out)

	mock.ExpectQuery(`UPDATE "users" SET "name" = $1 WHERE "id" = $2 RETURNING "id", "name"`).WithArgs("ann", 1).
		WillReturnRows(people(1))
	require.NoError(
		t,
		conn.Update("users").SetMap(m("name", "ann")).Where("id", 1).Returning("id", "name").Get(ctx, &out),
	)

	mock.ExpectQuery(`DELETE FROM "users" WHERE "id" = $1 RETURNING "id", "name"`).WithArgs(1).
		WillReturnRows(people(1))
	require.NoError(t, conn.Delete("users").Where("id", 1).Returning("id", "name").Get(ctx, &out))

	// Returning(...) is mandatory
	require.ErrorIs(t, conn.Insert("users").Values(m("name", "ann")).Get(ctx, &out), db.ErrInvalidArgs)
	require.ErrorIs(
		t,
		db.Use(db.Postgres).Insert("users").Values(m("a", 1)).Returning("id").Get(ctx, &out),
		db.ErrNotBound,
	)
	require.Error(t, conn.Insert("users").Returning("id").Get(ctx, &out))
}
