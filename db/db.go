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

// QueryEvent describes one executed statement, for hooks.
//
// Example:
//
//	hook := func(_ context.Context, ev db.QueryEvent) {
//		log.Println(ev.SQL, ev.NumArgs, ev.Duration, ev.Err)
//	}
//	conn := db.New(sqlDB, db.Postgres, db.WithHook(hook))
//	// ev.Args stays nil unless db.WithLogArgs() is also passed
type QueryEvent struct {
	SQL      string
	NumArgs  int
	Args     []any // only set when WithLogArgs is enabled: bind values may be personal data
	Duration time.Duration
	Err      error
}

// Option configures a DB.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithPrefix("app_"), db.WithLogArgs())
type Option func(*DB)

// WithPrefix sets a table prefix applied to every builder from DB.Table.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithPrefix("app_"))
//	conn.Table("users")
//	// SELECT * FROM "app_users"
func WithPrefix(prefix string) Option { return func(db *DB) { db.prefix = prefix } }

// WithHook registers fn to run after every statement (logging, metrics, tracing).
// Bind values are omitted from the event unless WithLogArgs is also set.
//
// Example:
//
//	hook := func(_ context.Context, ev db.QueryEvent) { log.Println(ev.SQL, ev.NumArgs) }
//	conn := db.New(sqlDB, db.Postgres, db.WithHook(hook))
//	conn.Table("users").Where("name", "ann").Count(ctx)
//	// hook sees: SELECT COUNT(*) AS "aggregate" FROM "users" WHERE "name" = $1, 1
func WithHook(fn func(context.Context, QueryEvent)) Option { return func(db *DB) { db.hook = fn } }

// WithLogArgs includes bind values in QueryEvent.Args. Off by default because
// arguments routinely carry personal data (UU PDP / ISO 27701).
//
// Example:
//
//	hook := func(_ context.Context, ev db.QueryEvent) { log.Println(ev.Args) }
//	conn := db.New(sqlDB, db.Postgres, db.WithHook(hook), db.WithLogArgs())
//	// ev.Args = ["ann"] for Where("name", "ann")
func WithLogArgs() Option { return func(db *DB) { db.logArgs = true } }

// DB executes builders against a connection. It is safe for concurrent use when
// the underlying Conn is (as *sql.DB is); a DB made by Transaction is not.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres)
//	n, err := conn.Table("users").Count(ctx)
type DB struct {
	conn    Conn
	dialect Dialect
	prefix  string
	hook    func(context.Context, QueryEvent)
	logArgs bool
	inTx    bool
	spSeq   *int
}

// New wraps conn (*sql.DB, *sql.Tx, *sql.Conn or any Conn) for the given dialect.
//
// Example:
//
//	conn := db.New(sqlDB, db.Postgres, db.WithPrefix("app_")) // sqlDB is a *sql.DB
//	n, err := conn.Table("users").Count(ctx)
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
// chain from Table:
//
//	sql, args, err := db.Use(db.Postgres).Table("users").Where("id", 7).ToSQL()
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

// NewQuery returns an empty builder bound to this DB, for sources that are not a
// plain table (FromSub, FromRaw).
//
// Example:
//
//	sub := conn.Table("orders").Select("user_id").Where("total", ">", 100)
//	sqlText, args, err := conn.NewQuery().FromSub(sub, "big").ToSQL()
//	// SELECT * FROM (SELECT "user_id" FROM "orders" WHERE "total" > $1) AS "big"   args: [100]
func (db *DB) NewQuery() *Builder {
	b := NewBuilder(db.dialect)
	b.prefix, b.db = db.prefix, db
	return b
}

// Table starts a builder bound to this DB.
//
// Example:
//
//	sqlText, args, err := conn.Table("users").Where("id", 7).ToSQL()
//	// SELECT * FROM "users" WHERE "id" = $1   args: [7]
func (db *DB) Table(name string) *Builder { return db.NewQuery().From(name) }

func (db *DB) query(ctx context.Context, sqlText string, args []any) (*sql.Rows, error) {
	if db.conn == nil {
		return nil, ErrNotBound
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
	start := time.Now()
	res, err := db.conn.ExecContext(ctx, sqlText, args...)
	db.emit(ctx, sqlText, args, start, err)
	return res, err
}

func (db *DB) emit(ctx context.Context, sqlText string, args []any, start time.Time, err error) {
	if db.hook == nil {
		return
	}
	ev := QueryEvent{SQL: sqlText, NumArgs: len(args), Duration: time.Since(start), Err: err}
	if db.logArgs {
		ev.Args = args
	}
	db.hook(ctx, ev)
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
//		_, err := tx.Table("users").Count(ctx)
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
		// fn panicked: undo, then keep unwinding.
		_ = rollback()
		panic(recover())
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

func (b *Builder) bound() (*DB, error) {
	if b.err != nil {
		return nil, b.err
	}
	if b.db == nil || b.db.conn == nil {
		return nil, ErrNotBound
	}
	return b.db, nil
}
