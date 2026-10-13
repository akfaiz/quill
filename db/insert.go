package db

import (
	"context"
	"fmt"
	"reflect"

	"github.com/akfaiz/quill/internal/rowmap"
)

// InsertBuilder is a mutable INSERT statement. Start one with DB.Insert; add rows
// with Values or Rows (or Using for INSERT ... SELECT) and finish with Exec, Get
// or ToSQL. Not safe for concurrent use.
type InsertBuilder struct {
	q         query
	rows      []map[string]any
	ignore    bool
	hasUsing  bool
	usingCols []string
	usingSub  *query
	upsert    *upsertSpec
}

type upsertSpec struct{ uniqueBy, update []string }

// Values appends rows, each a column map. Every row must use the same columns.
//
// Example:
//
//	conn.Insert("users").Values(map[string]any{"name": "Ann"}, map[string]any{"name": "Bob"})
//	// INSERT INTO "users" ("name") VALUES ($1), ($2)   args: [Ann Bob]
func (i *InsertBuilder) Values(rows ...map[string]any) *InsertBuilder {
	i.rows = append(i.rows, rows...)
	return i
}

// Rows appends structs (or slices of structs) as rows, using the same `db` tags as
// Row. An `omitempty` column is left out only when it is zero in every row of one
// Rows call, since a batch insert cannot skip a column for just some rows.
//
// Example:
//
//	conn.Insert("posts").Rows(Post{Title: "One"}, Post{Title: "Two"})
//	// INSERT INTO "posts" ("title") VALUES ($1), ($2)   args: [One Two]
func (i *InsertBuilder) Rows(structs ...any) *InsertBuilder {
	var (
		typ  reflect.Type
		vals []reflect.Value
	)
	add := func(v reflect.Value) error {
		if !v.IsValid() {
			return fmt.Errorf("%w: nil row", ErrInvalidArgs)
		}
		cp := reflect.New(v.Type()).Elem()
		cp.Set(v)
		t, err := rowmap.StructType(cp.Type())
		if err != nil {
			return err
		}
		if typ != nil && typ != t {
			return fmt.Errorf("%w: Rows needs one struct type, got %s and %s", ErrInvalidArgs, typ, t)
		}
		typ = t
		sv, err := rowmap.StructValue(cp)
		if err != nil {
			return fmt.Errorf("row %d: %w", len(vals), err)
		}
		vals = append(vals, sv)
		return nil
	}
	for _, s := range structs {
		rv := reflect.ValueOf(s)
		if rv.IsValid() && (rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array) {
			for k := range rv.Len() {
				if err := add(rv.Index(k)); err != nil {
					return i.fail(err)
				}
			}
			continue
		}
		if err := add(rv); err != nil {
			return i.fail(err)
		}
	}
	if len(vals) == 0 {
		return i
	}
	rows, err := rowmap.ToRows(typ, vals)
	if err != nil {
		return i.fail(err)
	}
	i.rows = append(i.rows, rows...)
	return i
}

// Ignore skips rows that conflict instead of failing: INSERT IGNORE on MySQL,
// ON CONFLICT DO NOTHING on Postgres and SQLite.
//
// Example:
//
//	conn.Insert("users").Values(map[string]any{"email": "a@x.io"}).Ignore()
//	// INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT DO NOTHING
func (i *InsertBuilder) Ignore() *InsertBuilder { i.ignore = true; return i }

// Using inserts the rows of a SELECT into columns (INSERT ... SELECT).
//
// Example:
//
//	conn.Insert("archived_users").Using([]string{"name"}, conn.Select("name").From("users"))
//	// INSERT INTO "archived_users" ("name") SELECT "name" FROM "users"   args: []
func (i *InsertBuilder) Using(columns []string, sub *SelectBuilder) *InsertBuilder {
	i.hasUsing, i.usingCols, i.usingSub = true, columns, sq(sub)
	return i
}

// Upsert updates on conflict. update lists the columns that take the inserted value
// (nil means every inserted column). uniqueBy is required on Postgres/SQLite (the
// conflict target) and ignored on MySQL.
//
// Example:
//
//	conn.Insert("users").Values(map[string]any{"email": "a@x.io", "name": "Ann"}).
//		Upsert([]string{"email"}, []string{"name"})
//	// MySQL: INSERT INTO `users` (`email`, `name`) VALUES (?, ?)
//	//        ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)   args: [a@x.io Ann]
func (i *InsertBuilder) Upsert(uniqueBy, update []string) *InsertBuilder {
	i.upsert = &upsertSpec{uniqueBy: uniqueBy, update: update}
	return i
}

func (i *InsertBuilder) fail(err error) *InsertBuilder { i.q.fail(err); return i }

// Clone returns an independent copy. Cloning a nil *InsertBuilder returns nil.
//
// Example:
//
//	base := conn.Insert("users").Values(map[string]any{"name": "Ann"})
//	withID := base.Clone().Returning("id")
func (i *InsertBuilder) Clone() *InsertBuilder {
	if i == nil {
		return nil
	}
	n := *i
	n.q = *i.q.Clone()
	n.rows = append([]map[string]any(nil), i.rows...)
	n.usingCols = append([]string(nil), i.usingCols...)
	n.usingSub = i.usingSub.Clone()
	if i.upsert != nil {
		u := *i.upsert
		n.upsert = &u
	}
	return &n
}

// ToSQL compiles the INSERT into SQL text and its ordered bind arguments.
// No rows returns ErrNoValues.
//
// Example:
//
//	sqlText, args, err := conn.Insert("users").Values(map[string]any{"name": "Ann"}).ToSQL()
//	// INSERT INTO "users" ("name") VALUES ($1)   args: [Ann]
func (i *InsertBuilder) ToSQL() (string, []any, error) {
	switch {
	case i.hasUsing && i.upsert != nil:
		return "", nil, fmt.Errorf("%w: Using cannot be combined with Upsert", ErrInvalidArgs)
	case i.hasUsing:
		if i.ignore {
			return i.q.insertOrIgnoreUsingSQL(i.usingCols, i.usingSub)
		}
		return i.q.insertUsingSQL(i.usingCols, i.usingSub)
	case i.upsert != nil && i.ignore:
		return "", nil, fmt.Errorf("%w: Ignore cannot be combined with Upsert", ErrInvalidArgs)
	case i.upsert != nil:
		return i.q.upsertSQL(i.rows, i.upsert.uniqueBy, i.upsert.update)
	case i.ignore:
		return i.q.insertOrIgnoreSQL(i.rows...)
	}
	return i.q.insertSQL(i.rows...)
}

// IDSQL compiles a single-row insert that returns the generated key: RETURNING on
// Postgres (sequence names the column, default "id"), a plain insert elsewhere.
//
// Example:
//
//	conn.Insert("users").Values(map[string]any{"name": "Ann"}).IDSQL()
//	// Postgres: INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"   args: [Ann]
func (i *InsertBuilder) IDSQL(sequence ...string) (string, []any, error) {
	if len(i.rows) != 1 {
		return "", nil, fmt.Errorf("%w: ExecGetID needs exactly one row, got %d", ErrInvalidArgs, len(i.rows))
	}
	return i.q.insertGetIDSQL(i.rows[0], sequence...)
}

// Exec runs the insert and returns the rows affected. No rows is a no-op.
//
// Example:
//
//	n, err := conn.Insert("users").Values(map[string]any{"name": "Ann"}).Exec(ctx)
//	// INSERT INTO "users" ("name") VALUES ($1)   args: [Ann]   (n = 1)
func (i *InsertBuilder) Exec(ctx context.Context) (int64, error) {
	return noop(i.q.execSQL(ctx, i.ToSQL))
}

// ExecGetID runs a single-row insert and returns the generated key (see IDSQL).
//
// Example:
//
//	id, err := conn.Insert("users").Values(map[string]any{"name": "Ann"}).ExecGetID(ctx)
//	// Postgres: INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"   (id = 12)
func (i *InsertBuilder) ExecGetID(ctx context.Context, sequence ...string) (int64, error) {
	if _, err := i.q.bound(); err != nil {
		return 0, err
	}
	sqlText, args, err := i.IDSQL(sequence...)
	if err != nil {
		return 0, err
	}
	return i.q.insertID(ctx, sqlText, args)
}

// Get runs the insert and scans the RETURNING rows into dest (a pointer to a slice
// of structs, maps or scalars). Call Returning first. Postgres and SQLite >= 3.35.
//
// Example:
//
//	var created []User
//	err := conn.Insert("users").Values(map[string]any{"name": "Ann"}).Returning("*").Get(ctx, &created)
//	// INSERT INTO "users" ("name") VALUES ($1) RETURNING *   args: [Ann]
func (i *InsertBuilder) Get(ctx context.Context, dest any) error {
	return i.q.returningInto(ctx, dest, i.ToSQL)
}
