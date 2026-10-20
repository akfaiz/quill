package db_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"runtime"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
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
			sql, args, err := db.Use(tc.d).Select().From("users").
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
	a, _, _ := pg.Select().From("a").Where("x", 1).ToSQL()
	b, _, _ := pg.Select().From("b").Limit(1).ToSQL()
	if a != `SELECT * FROM "a" WHERE "x" = $1` || b != `SELECT * FROM "b" LIMIT 1` {
		t.Errorf("builders leaked into each other:\n a: %s\n b: %s", a, b)
	}
	// NewQuery gives a table-less builder for subquery sources
	sql, _, err := pg.Select().FromSub(pg.Select().From("inner").Where("k", 1), "i").ToSQL()
	if err != nil || sql != `SELECT * FROM (SELECT * FROM "inner" WHERE "k" = $1) AS "i"` {
		t.Errorf("got %s (%v)", sql, err)
	}
}

func TestUseAppliesOptions(t *testing.T) {
	sql, _, err := db.Use(db.MySQL, db.WithPrefix("app_")).Select().From("users as u").Where("u.id", 1).ToSQL()
	require.NoError(t, err)
	if want := "SELECT * FROM `app_users` AS `app_u` WHERE `app_u`.`id` = ?"; sql != want {
		t.Errorf("got %s\nwant %s", sql, want)
	}
}

func TestUseWithAnUnknownDialectFailsAtTheTerminalCall(t *testing.T) {
	q := db.Use(db.Dialect(99)).Select().From("users").Where("id", 1) // the chain itself keeps working
	if _, _, err := q.ToSQL(); !errors.Is(err, db.ErrUnknownDialect) {
		t.Errorf("got %v, want ErrUnknownDialect", err)
	}
}

func TestUseCannotRunQueriesButNeverPanics(t *testing.T) {
	ctx := context.Background()
	conn := db.Use(db.Postgres)
	users := func() *db.SelectBuilder { return conn.Select().From("users") }
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
		"DB.Query":    func() error { return conn.Query(ctx, &rows, "SELECT 1") },
		"DB.Exec":     func() error { _, err := conn.Exec(ctx, "SELECT 1"); return err },
		"Transaction": func() error { return conn.Transaction(ctx, func(*db.DB) error { return nil }) },
		"typed Get":   func() error { _, err := db.From[chainRow](conn).Get(ctx); return err },
		"Insert":      func() error { _, err := conn.Insert("users").Values(map[string]any{"a": 1}).Exec(ctx); return err },
		"InsertGetID": func() error { _, err := conn.Insert("users").Values(map[string]any{"a": 1}).ExecGetID(ctx); return err },
		"Update": func() error {
			_, err := conn.Update("users").Set("a", 1).Where("id", 1).Exec(ctx)
			return err
		},
		"Delete":   func() error { _, err := conn.Delete("users").Where("id", 1).Exec(ctx); return err },
		"Truncate": func() error { return conn.Truncate(ctx, "users") },
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

func TestDialectAndRawQueryExec(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.MySQL)
	assert.Equal(t, db.MySQL, conn.Dialect())

	mock.ExpectQuery("SELECT id, name FROM people WHERE id = ?").WithArgs(1).WillReturnRows(people(1))
	var out []person
	require.NoError(t, conn.Query(ctx, &out, "SELECT id, name FROM people WHERE id = ?", 1))
	assert.Equal(t, []person{{1, "p1"}}, out)

	mock.ExpectQuery("SELECT 1").WillReturnError(errors.New("boom"))
	require.Error(t, conn.Query(ctx, &out, "SELECT 1"))

	mock.ExpectExec("UPDATE people SET x = ?").WithArgs(1).WillReturnResult(sqlmock.NewResult(0, 3))
	n, err := conn.Exec(ctx, "UPDATE people SET x = ?", 1)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)

	mock.ExpectExec("DELETE FROM people").WillReturnError(errors.New("boom"))
	_, err = conn.Exec(ctx, "DELETE FROM people")
	require.Error(t, err)
}

func TestHookSeesEveryStatementAndHidesArgsByDefault(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	var events []db.QueryEvent
	hook := func(_ context.Context, ev db.QueryEvent) { events = append(events, ev) }

	conn := db.New(sqlDB, db.Postgres, db.WithHook(hook))
	mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "people" WHERE "name" = $1`).WithArgs("ann").
		WillReturnRows(count(1))
	_, err := conn.Select().From("people").Where("name", "ann").Count(ctx)
	require.NoError(t, err)
	mock.ExpectExec(`DELETE FROM "people" WHERE "id" = $1`).WithArgs(1).WillReturnError(errors.New("boom"))
	_, err = conn.Delete("people").Where("id", 1).Exec(ctx)
	require.Error(t, err)

	require.Len(t, events, 2)
	assert.Equal(t, 1, events[0].NumArgs)
	assert.Nil(t, events[0].Args, "bind values are personal data: omitted unless WithLogArgs")
	require.NoError(t, events[0].Err)
	require.Error(t, events[1].Err)

	events = nil
	logged := db.New(sqlDB, db.Postgres, db.WithHook(hook), db.WithLogArgs())
	mock.ExpectQuery(`SELECT * FROM "people" WHERE "name" = $1`).WithArgs("ann").WillReturnRows(people(0))
	var out []person
	require.NoError(t, logged.Select().From("people").Where("name", "ann").Get(ctx, &out))
	require.Len(t, events, 1)
	assert.Equal(t, []any{"ann"}, events[0].Args)
}

func TestBeforeAndAfterHooks(t *testing.T) {
	type ctxKey struct{}
	ctx := context.Background()

	t.Run("order, context threading and event contents", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		var trace []string
		var beforeEv, afterEv db.QueryEvent
		var afterCtxVal any
		conn := db.New(sqlDB, db.Postgres,
			db.WithBeforeHook(func(c context.Context, ev db.QueryEvent) (context.Context, error) {
				trace = append(trace, "before1")
				beforeEv = ev
				return context.WithValue(c, ctxKey{}, "span"), nil
			}),
			db.WithBeforeHook(func(c context.Context, _ db.QueryEvent) (context.Context, error) {
				trace = append(trace, "before2:"+c.Value(ctxKey{}).(string))
				return c, nil
			}),
			db.WithAfterHook(func(c context.Context, ev db.QueryEvent) {
				trace = append(trace, "after")
				afterEv, afterCtxVal = ev, c.Value(ctxKey{})
			}),
		)
		mock.ExpectExec(`DELETE FROM "people" WHERE "id" = $1`).WithArgs(1).WillReturnResult(sqlmock.NewResult(0, 1))
		_, err := conn.Delete("people").Where("id", 1).Exec(ctx)
		require.NoError(t, err)

		assert.Equal(t, []string{"before1", "before2:span", "after"}, trace)
		assert.Equal(t, `DELETE FROM "people" WHERE "id" = $1`, beforeEv.SQL)
		assert.Equal(t, 1, beforeEv.NumArgs)
		assert.Nil(t, beforeEv.Args)
		assert.Zero(t, beforeEv.Duration)
		assert.Equal(t, "span", afterCtxVal)
		assert.NoError(t, afterEv.Err)
	})
	t.Run("before error aborts the statement", func(t *testing.T) {
		sqlDB, _ := newMock(t) // no expectations: any statement sent would fail the mock
		boom := errors.New("blocked")
		afterRan := false
		conn := db.New(sqlDB, db.Postgres,
			db.WithBeforeHook(func(c context.Context, _ db.QueryEvent) (context.Context, error) { return c, boom }),
			db.WithAfterHook(func(context.Context, db.QueryEvent) { afterRan = true }),
		)
		_, err := conn.Exec(ctx, "DELETE FROM people")
		require.ErrorIs(t, err, boom)
		var out []person
		require.ErrorIs(t, conn.Query(ctx, &out, "SELECT * FROM people"), boom)
		assert.False(t, afterRan)
	})
	t.Run("args follow WithLogArgs", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		var got []any
		conn := db.New(sqlDB, db.Postgres, db.WithLogArgs(),
			db.WithBeforeHook(func(c context.Context, ev db.QueryEvent) (context.Context, error) {
				got = ev.Args
				return c, nil
			}))
		mock.ExpectExec("UPDATE a SET x = $1").WithArgs(5).WillReturnResult(sqlmock.NewResult(0, 1))
		_, err := conn.Exec(ctx, "UPDATE a SET x = $1", 5)
		require.NoError(t, err)
		assert.Equal(t, []any{5}, got)
	})
}

func TestTransactionCommitsRollsBackAndRepanics(t *testing.T) {
	ctx := context.Background()

	t.Run("commit", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE a SET x = 1").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		err := db.New(sqlDB, db.Postgres).Transaction(ctx, func(tx *db.DB) error {
			_, err := tx.Exec(ctx, "UPDATE a SET x = 1")
			return err
		})
		require.NoError(t, err)
	})
	t.Run("rollback on error", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		boom := errors.New("boom")
		mock.ExpectBegin()
		mock.ExpectRollback()
		err := db.New(sqlDB, db.Postgres).Transaction(ctx, func(*db.DB) error { return boom })
		require.ErrorIs(t, err, boom)
	})
	t.Run("rollback failure is joined", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		boom, rbBoom := errors.New("boom"), errors.New("rollback boom")
		mock.ExpectBegin()
		mock.ExpectRollback().WillReturnError(rbBoom)
		err := db.New(sqlDB, db.Postgres).Transaction(ctx, func(*db.DB) error { return boom })
		require.ErrorIs(t, err, boom)
		require.ErrorIs(t, err, rbBoom)
	})
	t.Run("rollback on panic then re-panic", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		mock.ExpectBegin()
		mock.ExpectRollback()
		assert.PanicsWithValue(t, "kaboom", func() {
			_ = db.New(sqlDB, db.Postgres).Transaction(ctx, func(*db.DB) error { panic("kaboom") })
		})
	})
	t.Run("rollback on Goexit without a nil panic", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		mock.ExpectBegin()
		mock.ExpectRollback()
		var recovered any
		done := make(chan struct{})
		go func() {
			defer close(done)
			defer func() { recovered = recover() }()
			_ = db.New(sqlDB, db.Postgres).Transaction(ctx, func(*db.DB) error {
				runtime.Goexit() // what t.FailNow does inside the callback
				return nil
			})
		}()
		<-done
		assert.Nil(t, recovered)
	})
	t.Run("begin failure", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		mock.ExpectBegin().WillReturnError(errors.New("no tx"))
		err := db.New(sqlDB, db.Postgres).Transaction(ctx, func(*db.DB) error { return nil })
		require.Error(t, err)
	})
	t.Run("options reach BeginTx", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		mock.ExpectBegin()
		mock.ExpectCommit()
		opts := &sql.TxOptions{ReadOnly: true}
		require.NoError(t, db.New(sqlDB, db.Postgres).TransactionOpts(ctx, opts, func(*db.DB) error { return nil }))
	})
	t.Run("commit failure", func(t *testing.T) {
		sqlDB, mock := newMock(t)
		mock.ExpectBegin()
		mock.ExpectCommit().WillReturnError(errors.New("commit boom"))
		err := db.New(sqlDB, db.Postgres).Transaction(ctx, func(*db.DB) error { return nil })
		require.Error(t, err)
	})
}

func TestNestedTransactionUsesSavepoints(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec("SAVEPOINT sp_1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("RELEASE SAVEPOINT sp_1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("SAVEPOINT sp_2").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("ROLLBACK TO SAVEPOINT sp_2").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	boom := errors.New("boom")
	err := db.New(sqlDB, db.Postgres).Transaction(ctx, func(tx *db.DB) error {
		require.NoError(t, tx.Transaction(ctx, func(*db.DB) error { return nil }))
		require.ErrorIs(t, tx.Transaction(ctx, func(*db.DB) error { return boom }), boom)
		return nil
	})
	require.NoError(t, err)
}

func TestSavepointFailures(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec("SAVEPOINT sp_1").WillReturnError(errors.New("sp boom"))
	mock.ExpectRollback()
	err := db.New(sqlDB, db.Postgres).Transaction(ctx, func(tx *db.DB) error {
		return tx.Transaction(ctx, func(*db.DB) error { return nil })
	})
	require.Error(t, err)
}

func TestTransactionNeedsABeginner(t *testing.T) {
	ctx := context.Background()
	err := db.New(noTx{}, db.Postgres).Transaction(ctx, func(*db.DB) error { return nil })
	require.ErrorIs(t, err, db.ErrNoTransaction)
}

// noTx is a Conn that cannot begin transactions.
type noTx struct{}

func (noTx) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	return nil, errors.New("unused")
}

func (noTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unused")
}

func TestNewOnAnExistingSQLTxUsesSavepoints(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	mock.ExpectBegin()
	mock.ExpectExec("SAVEPOINT sp_1").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("RELEASE SAVEPOINT sp_1").WillReturnResult(sqlmock.NewResult(0, 0))
	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, db.New(tx, db.Postgres).Transaction(ctx, func(*db.DB) error { return nil }))
}

type chainRow struct{ ID int64 }

func (chainRow) TableName() string { return "t" }
