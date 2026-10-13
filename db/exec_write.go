package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"sort"

	"github.com/akfaiz/quill/internal/rowmap"
)

// execSQL runs a write statement and returns the rows affected.
func (b *query) execSQL(ctx context.Context, build func() (string, []any, error)) (int64, error) {
	db, err := b.bound()
	if err != nil {
		return 0, err
	}
	sqlText, args, err := build()
	if err != nil {
		return 0, err
	}
	res, err := db.exec(ctx, sqlText, args)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// noop turns "nothing to write" into a successful no-op.
func noop(n int64, err error) (int64, error) {
	if errors.Is(err, ErrNoValues) {
		return 0, nil
	}
	return n, err
}

// insertID runs an insert and returns the generated key: the RETURNING value on
// Postgres, LastInsertId elsewhere.
func (b *query) insertID(ctx context.Context, sqlText string, args []any) (int64, error) {
	db, err := b.bound()
	if err != nil {
		return 0, err
	}
	if db.dialect == Postgres {
		var id int64
		err = db.runSQL(ctx, sqlText, args, func(rows *sql.Rows) error { return rowmap.First(rows, &id) })
		return id, err
	}
	res, err := db.exec(ctx, sqlText, args)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (b *query) runTruncate(ctx context.Context) error {
	db, err := b.bound()
	if err != nil {
		return err
	}
	stmts, err := b.truncateSQL()
	if err != nil {
		return err
	}
	if len(stmts) == 1 {
		_, err = db.exec(ctx, stmts[0].SQL, stmts[0].Args)
		return err
	}
	// SQLite: reset the AUTOINCREMENT counter and empty the table, or neither.
	c, err := b.writeCompiler()
	if err != nil {
		return err
	}
	probe := "SELECT 1 FROM " + c.sqliteSchema(
		b.table,
	) + "sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'"
	return db.Transaction(ctx, func(tx *DB) error {
		hasSeq := false
		if err := tx.runSQL(ctx, probe, nil, func(rows *sql.Rows) error {
			hasSeq = rows.Next()
			return rows.Err()
		}); err != nil {
			return err
		}
		for i, s := range stmts {
			if i == 0 && !hasSeq {
				continue // sqlite_sequence appears with the first AUTOINCREMENT table
			}
			if _, err := tx.exec(ctx, s.SQL, s.Args); err != nil {
				return err
			}
		}
		return nil
	})
}

func (b *query) returningInto(ctx context.Context, dest any, build func() (string, []any, error)) error {
	db, err := b.bound()
	if err != nil {
		return err
	}
	if len(b.returning) == 0 {
		return fmt.Errorf("%w: call Returning(...) first", ErrInvalidArgs)
	}
	sqlText, args, err := build()
	if err != nil {
		return err
	}
	return db.runSQL(ctx, sqlText, args, func(rows *sql.Rows) error { return rowmap.Slice(rows, dest) })
}

// Truncate empties a table and resets its auto-increment counter. On SQLite
// this runs as DELETE plus a sqlite_sequence reset inside a transaction.
//
// Example:
//
//	err := conn.Truncate(ctx, "sessions")
//	// Postgres: TRUNCATE "sessions" RESTART IDENTITY   MySQL: TRUNCATE TABLE `sessions`
func (db *DB) Truncate(ctx context.Context, table string) error {
	return db.truncateQuery(table).runTruncate(ctx)
}

// TruncateSQL builds the statements Truncate runs, in order.
//
// Example:
//
//	stmts, err := conn.TruncateSQL("sessions")
//	// Postgres: [TRUNCATE "sessions" RESTART IDENTITY]
func (db *DB) TruncateSQL(table string) ([]Stmt, error) {
	return db.truncateQuery(table).truncateSQL()
}

func (db *DB) truncateQuery(table string) *query {
	q := &query{}
	db.initQuery(q)
	return q.From(table)
}

// UpdateOrInsert updates the first row matching attributes with values, or inserts
// attributes merged with values when none matches. It reports whether the write
// succeeded.
//
// Example:
//
//	ok, err := conn.UpdateOrInsert(ctx, "users",
//		map[string]any{"email": "ann@example.com"}, map[string]any{"name": "Ann"})
//	// SELECT EXISTS(SELECT * FROM "users" WHERE "email" = $1)
//	// then UPDATE "users" SET "name" = $1 WHERE "email" = $2 (LIMIT 1), or INSERT when absent
func (db *DB) UpdateOrInsert(ctx context.Context, table string, attributes, values map[string]any) (bool, error) {
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	find := db.Select().From(table)
	for _, k := range keys {
		find.Where(k, attributes[k])
	}
	exists, err := find.Exists(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		merged := make(map[string]any, len(attributes)+len(values))
		maps.Copy(merged, attributes)
		maps.Copy(merged, values)
		_, err = db.Insert(table).Values(merged).Exec(ctx)
		return err == nil, err
	}
	if len(values) == 0 {
		return true, nil
	}
	upd := db.Update(table).SetMap(values).Limit(1)
	for _, k := range keys {
		upd.Where(k, attributes[k])
	}
	_, err = upd.Exec(ctx)
	return err == nil, err
}

var _ = fmt.Errorf
