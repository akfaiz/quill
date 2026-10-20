package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/akfaiz/quill/internal/rowmap"
)

var (
	// ErrNotFound is returned by First/Find/Sole/Value when no row matches.
	// It is sql.ErrNoRows, so errors.Is(err, sql.ErrNoRows) also works.
	ErrNotFound = sql.ErrNoRows
	// ErrMultipleRecords is returned by Sole when more than one row matches.
	ErrMultipleRecords = errors.New("quill: multiple records found")
	// ErrNoOrder is returned by Chunk when the query has no ORDER BY.
	ErrNoOrder = errors.New("quill: chunking requires an order by clause")
	// ErrNotBound is returned when running a query on a DB that has no connection,
	// such as one from Use. Create the DB with New(conn, dialect) to execute queries.
	ErrNotBound = errors.New("quill: no database connection; create the DB with db.New(conn, dialect) to run queries")
	// ErrNoTransaction is returned by Transaction when the connection cannot begin one.
	ErrNoTransaction = errors.New("quill: connection cannot begin transactions")
)

// Conn is the subset of *sql.DB, *sql.Tx and *sql.Conn the executor needs.
//
// Example:
//
//	sqlDB, err := sql.Open("pgx", dsn) // *sql.DB, *sql.Tx and *sql.Conn all satisfy Conn
//	conn := db.New(sqlDB, db.Postgres)
type Conn interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type beginner interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// QueryEvent describes one statement, for hooks. A before hook sees SQL, NumArgs and
// Args only; Duration and Err are filled for after hooks.
//
// Example:
//
//	hook := func(_ context.Context, ev db.QueryEvent) {
//		log.Println(ev.SQL, ev.NumArgs, ev.Duration, ev.Err)
//	}
//	conn := db.New(sqlDB, db.Postgres, db.WithAfterHook(hook))
//	// ev.Args stays nil unless db.WithLogArgs() is also passed
type QueryEvent struct {
	SQL      string
	NumArgs  int
	Args     []any // only set when WithLogArgs is enabled: bind values may be personal data
	Duration time.Duration
	Err      error
}

// BeforeHook runs before a statement is sent. The returned context replaces ctx for the
// statement and for the after hooks (start a trace span here, finish it in an after hook).
// A non-nil error aborts the statement: it is returned to the caller, nothing is sent and
// after hooks do not run.
type BeforeHook func(ctx context.Context, ev QueryEvent) (context.Context, error)

// AfterHook runs after a statement finished, whether it succeeded or not.
type AfterHook func(ctx context.Context, ev QueryEvent)

// Option configures a DB.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithPrefix("app_"), db.WithLogArgs())
type Option func(*DB)

// WithPrefix sets a table prefix applied to every statement builder created from the DB.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithPrefix("app_"))
//	conn.Select().From("users")
//	// SELECT * FROM "app_users"
func WithPrefix(prefix string) Option { return func(db *DB) { db.prefix = prefix } }

// WithHook registers fn to run after every statement (logging, metrics, tracing).
// It is the same as WithAfterHook. Bind values are omitted from the event unless
// WithLogArgs is also set.
//
// Example:
//
//	hook := func(_ context.Context, ev db.QueryEvent) { log.Println(ev.SQL, ev.NumArgs) }
//	conn := db.New(sqlDB, db.Postgres, db.WithHook(hook))
//	conn.Select().From("users").Where("name", "ann").Count(ctx)
//	// hook sees: SELECT COUNT(*) AS "aggregate" FROM "users" WHERE "name" = $1, 1
func WithHook(fn func(context.Context, QueryEvent)) Option { return WithAfterHook(fn) }

// WithAfterHook registers fn to run after every statement, with Duration and Err set.
// Hooks run in registration order. Bind values are omitted unless WithLogArgs is set.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithAfterHook(func(_ context.Context, ev db.QueryEvent) {
//		log.Println(ev.SQL, ev.Duration, ev.Err)
//	}))
func WithAfterHook(fn func(context.Context, QueryEvent)) Option {
	return func(db *DB) { db.after = append(db.after, fn) }
}

// WithBeforeHook registers fn to run before every statement is sent. Hooks run in
// registration order; each receives the context returned by the previous one. The first
// error aborts the statement (see BeforeHook). Bind values are omitted unless
// WithLogArgs is set.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithBeforeHook(func(ctx context.Context, ev db.QueryEvent) (context.Context, error) {
//		if strings.HasPrefix(ev.SQL, "DELETE") {
//			return ctx, errors.New("deletes are disabled")
//		}
//		return context.WithValue(ctx, spanKey{}, time.Now()), nil
//	}))
func WithBeforeHook(fn BeforeHook) Option {
	return func(db *DB) { db.before = append(db.before, fn) }
}

// WithLogArgs includes bind values in QueryEvent.Args. Off by default because
// arguments routinely carry personal data (UU PDP / ISO 27701).
//
// Example:
//
//	hook := func(_ context.Context, ev db.QueryEvent) { log.Println(ev.Args) }
//	conn := db.New(sqlDB, db.Postgres, db.WithHook(hook), db.WithLogArgs())
//	// ev.Args = ["ann"] for Where("name", "ann")
func WithLogArgs() Option { return func(db *DB) { db.logArgs = true } }

// DB executes statements against a connection. It is safe for concurrent use when
// the underlying Conn is (as *sql.DB is); a DB made by Transaction is not.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres)
//	n, err := conn.Select().From("users").Count(ctx)
type DB struct {
	conn    Conn
	dialect Dialect
	prefix  string
	before  []BeforeHook
	after   []AfterHook
	logArgs bool
	inTx    bool
	spSeq   *int
}

// New wraps conn (*sql.DB, *sql.Tx, *sql.Conn or any Conn) for the given dialect.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithPrefix("app_")) // sqlDB is a *sql.DB
//	n, err := conn.Select().From("users").Count(ctx)
//	// SELECT COUNT(*) AS "aggregate" FROM "app_users"
func New(conn Conn, d Dialect, opts ...Option) *DB {
	db := &DB{conn: conn, dialect: d}
	if _, ok := conn.(*sql.Tx); ok {
		db.inTx, db.spSeq = true, new(int)
	}
	for _, o := range opts {
		o(db)
	}
	return db
}

// Use starts a connection-less DB for building SQL: set the dialect first, then
// chain from Select, Insert, Update or Delete:
//
//	sql, args, err := db.Use(db.Postgres).Select().From("users").Where("id", 7).ToSQL()
//
// The same options as New apply (WithPrefix). Running a query on it returns
// ErrNotBound; create the DB with New to execute.
func Use(d Dialect, opts ...Option) *DB { return New(nil, d, opts...) }

// Dialect returns the dialect this DB compiles for.
//
// Example:
//
//	conn := db.Use(db.MySQL)
//	conn.Dialect() == db.MySQL // true
func (db *DB) Dialect() Dialect { return db.dialect }

// initQuery readies q to compile for this DB's dialect and prefix and to run on its connection.
func (db *DB) initQuery(q *query) {
	q.dialect, q.prefix, q.db = db.dialect, db.prefix, db
	if _, err := db.dialect.grammar(); err != nil {
		q.err = err
	}
}

// Select starts a SELECT builder; columns are optional (default *).
//
// Example:
//
//	sqlText, args, err := conn.Select("id", "name").From("users").Where("id", 7).ToSQL()
//	// SELECT "id", "name" FROM "users" WHERE "id" = $1   args: [7]
func (db *DB) Select(columns ...string) *SelectBuilder {
	s := &SelectBuilder{}
	db.initQuery(&s.q)
	if len(columns) > 0 {
		s.q.Select(columns...)
	}
	return s
}

// Insert starts an INSERT builder for table.
//
// Example:
//
//	sqlText, args, err := conn.Insert("users").Values(map[string]any{"name": "Ann"}).ToSQL()
//	// INSERT INTO "users" ("name") VALUES ($1)   args: [Ann]
func (db *DB) Insert(table string) *InsertBuilder {
	i := &InsertBuilder{}
	db.initQuery(&i.q)
	i.q.From(table)
	return i
}

// Update starts an UPDATE builder for table.
//
// Example:
//
//	sqlText, args, err := conn.Update("users").Set("name", "Ann").Where("id", 7).ToSQL()
//	// UPDATE "users" SET "name" = $1 WHERE "id" = $2   args: [Ann 7]
func (db *DB) Update(table string) *UpdateBuilder {
	u := &UpdateBuilder{}
	db.initQuery(&u.q)
	u.q.From(table)
	return u
}

// Delete starts a DELETE builder for table.
//
// Example:
//
//	sqlText, args, err := conn.Delete("users").Where("id", 7).ToSQL()
//	// DELETE FROM "users" WHERE "id" = $1   args: [7]
func (db *DB) Delete(table string) *DeleteBuilder {
	d := &DeleteBuilder{}
	db.initQuery(&d.q)
	d.q.From(table)
	return d
}

func (db *DB) query(ctx context.Context, sqlText string, args []any) (*sql.Rows, error) {
	if db.conn == nil {
		return nil, ErrNotBound
	}
	ctx, err := db.emitBefore(ctx, sqlText, args)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	rows, err := db.conn.QueryContext(ctx, sqlText, args...)
	db.emit(ctx, sqlText, args, start, err)
	return rows, err
}

// runSQL runs a query and passes its rows to fn, always closing them.
func (db *DB) runSQL(ctx context.Context, sqlText string, args []any, fn func(*sql.Rows) error) error {
	rows, err := db.query(ctx, sqlText, args)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	return fn(rows)
}

func (db *DB) exec(ctx context.Context, sqlText string, args []any) (sql.Result, error) {
	if db.conn == nil {
		return nil, ErrNotBound
	}
	ctx, err := db.emitBefore(ctx, sqlText, args)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	res, err := db.conn.ExecContext(ctx, sqlText, args...)
	db.emit(ctx, sqlText, args, start, err)
	return res, err
}

func (db *DB) event(sqlText string, args []any) QueryEvent {
	ev := QueryEvent{SQL: sqlText, NumArgs: len(args)}
	if db.logArgs {
		ev.Args = args
	}
	return ev
}

// emitBefore runs the before hooks, threading the context through them. The first error
// stops the chain and is returned.
func (db *DB) emitBefore(ctx context.Context, sqlText string, args []any) (context.Context, error) {
	if len(db.before) == 0 {
		return ctx, nil
	}
	ev := db.event(sqlText, args)
	for _, h := range db.before {
		var err error
		if ctx, err = h(ctx, ev); err != nil {
			return ctx, err
		}
	}
	return ctx, nil
}

func (db *DB) emit(ctx context.Context, sqlText string, args []any, start time.Time, err error) {
	if len(db.after) == 0 {
		return
	}
	ev := db.event(sqlText, args)
	ev.Duration, ev.Err = time.Since(start), err
	for _, h := range db.after {
		h(ctx, ev)
	}
}

// Query runs raw SQL (use the dialect's placeholders) and scans every row into
// dest, a pointer to a slice of structs, maps or scalars.
//
// Example:
//
//	var users []User
//	err := conn.Query(ctx, &users, "SELECT id, name FROM users WHERE id = $1", 1)
//	// sends the text as given with args: [1]; users = [{1 ann}]
func (db *DB) Query(ctx context.Context, dest any, sqlText string, args ...any) error {
	rows, err := db.query(ctx, sqlText, args)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	return rowmap.Slice(rows, dest)
}

// Exec runs a raw statement and returns the rows affected.
//
// Example:
//
//	n, err := conn.Exec(ctx, "UPDATE users SET active = $1 WHERE id < $2", false, 4)
//	// n = 3 rows affected
func (db *DB) Exec(ctx context.Context, sqlText string, args ...any) (int64, error) {
	res, err := db.exec(ctx, sqlText, args)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Transaction runs fn in a transaction: commit on nil, rollback on error or panic
// (the panic is re-raised). Inside an existing transaction it uses a savepoint.
//
// Example:
//
//	err := conn.Transaction(ctx, func(tx *db.DB) error {
//		_, err := tx.Exec(ctx, "UPDATE accounts SET balance = balance - $1 WHERE id = $2", 50, 1)
//		return err // nil commits, an error rolls back
//	})
//	// BEGIN, UPDATE ..., COMMIT
//	// nested call on tx: SAVEPOINT sp_1 ... RELEASE SAVEPOINT sp_1
func (db *DB) Transaction(ctx context.Context, fn func(tx *DB) error) error {
	return db.TransactionOpts(ctx, nil, fn)
}

// TransactionOpts is Transaction with sql.TxOptions (isolation level, read-only).
// opts is ignored for nested savepoints.
//
// Example:
//
//	opts := &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true}
//	err := conn.TransactionOpts(ctx, opts, func(tx *db.DB) error {
//		_, err := tx.Select().From("users").Count(ctx)
//		return err
//	})
func (db *DB) TransactionOpts(ctx context.Context, opts *sql.TxOptions, fn func(tx *DB) error) (err error) {
	if db.conn == nil {
		return ErrNotBound
	}
	if db.inTx {
		return db.savepoint(ctx, fn)
	}
	bg, ok := db.conn.(beginner)
	if !ok {
		return ErrNoTransaction
	}
	tx, err := bg.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	child := *db
	child.conn, child.inTx, child.spSeq = tx, true, new(int)
	return finishTx(&child, fn, tx.Commit, tx.Rollback)
}

func (db *DB) savepoint(ctx context.Context, fn func(tx *DB) error) error {
	*db.spSeq++
	name := fmt.Sprintf("sp_%d", *db.spSeq)
	if _, err := db.exec(ctx, "SAVEPOINT "+name, nil); err != nil {
		return err
	}
	return finishTx(db, fn,
		func() error { _, err := db.exec(ctx, "RELEASE SAVEPOINT "+name, nil); return err },
		func() error { _, err := db.exec(ctx, "ROLLBACK TO SAVEPOINT "+name, nil); return err },
	)
}

func finishTx(tx *DB, fn func(*DB) error, commit, rollback func() error) (err error) {
	done := false
	defer func() {
		if done {
			return
		}
		// fn panicked or called runtime.Goexit (t.FailNow): undo, then keep
		// unwinding. Goexit leaves nothing to recover, so only a panic is re-raised.
		_ = rollback()
		if r := recover(); r != nil {
			panic(r)
		}
	}()
	err = fn(tx)
	done = true
	if err != nil {
		if rbErr := rollback(); rbErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	return commit()
}

func (b *query) bound() (*DB, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.db == nil || b.db.conn == nil {
		return nil, ErrNotBound
	}
	return b.db, nil
}
