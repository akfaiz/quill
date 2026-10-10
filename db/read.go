package db

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"

	"github.com/akfaiz/quill/internal/rowmap"
)

// run executes the compiled SELECT and hands the rows to fn, always closing them.
func (b *Builder) run(ctx context.Context, fn func(rows *sql.Rows) error) error {
	db, err := b.bound()
	if err != nil {
		return err
	}
	sqlText, args, err := b.ToSQL()
	if err != nil {
		return err
	}
	return db.runSQL(ctx, sqlText, args, fn)
}

// Get runs the query and scans every row into dest: a pointer to a slice of
// structs (`db:"col"` tags, else snake_case field names), pointers to structs,
// map[string]any, or scalars (single column). NULL leaves a plain field at its zero
// value; use pointer or sql.Null* fields to tell NULL apart.
func (b *Builder) Get(ctx context.Context, dest any) error {
	return b.run(ctx, func(rows *sql.Rows) error { return rowmap.Slice(rows, dest) })
}

// First scans the first row into dest (*struct, *map[string]any or *scalar).
// It returns ErrNotFound when nothing matches.
func (b *Builder) First(ctx context.Context, dest any) error {
	return b.Clone().Limit(1).run(ctx, func(rows *sql.Rows) error { return rowmap.First(rows, dest) })
}

// Find is Where("id", id).First.
func (b *Builder) Find(ctx context.Context, id any, dest any) error {
	return b.Clone().Where("id", id).First(ctx, dest)
}

// Sole returns the only matching row: ErrNotFound for none, ErrMultipleRecords for several.
func (b *Builder) Sole(ctx context.Context, dest any) error {
	return b.Clone().Limit(2).run(ctx, func(rows *sql.Rows) error {
		if err := rowmap.First(rows, dest); err != nil {
			return err
		}
		if rows.Next() {
			return ErrMultipleRecords
		}
		return rows.Err()
	})
}

// Value scans a single column of the first row into dest (a pointer to a scalar).
func (b *Builder) Value(ctx context.Context, column string, dest any) error {
	return b.Clone().Select(column).Limit(1).run(ctx, func(rows *sql.Rows) error { return rowmap.First(rows, dest) })
}

// Pluck scans one column of every row into dest, a pointer to a slice of scalars.
func (b *Builder) Pluck(ctx context.Context, column string, dest any) error {
	return b.Clone().Select(column).Get(ctx, dest)
}

// PluckMap scans two columns into dest, a non-nil pointer to a map[K]V (key column -> value column).
func (b *Builder) PluckMap(ctx context.Context, column, key string, dest any) error {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Map {
		return fmt.Errorf("%w: PluckMap destination must be a pointer to a map, got %T", ErrInvalidArgs, dest)
	}
	mt := rv.Elem().Type()
	return b.Clone().Select(column, key).run(ctx, func(rows *sql.Rows) error {
		out := reflect.MakeMap(mt)
		for rows.Next() {
			v, k := reflect.New(mt.Elem()).Elem(), reflect.New(mt.Key()).Elem()
			vt, vpost := rowmap.Target(v)
			kt, kpost := rowmap.Target(k)
			if err := rows.Scan(vt, kt); err != nil {
				return err
			}
			for _, p := range []func(){vpost, kpost} {
				if p != nil {
					p()
				}
			}
			out.SetMapIndex(k, v)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rv.Elem().Set(out)
		return nil
	})
}

// ExistsSQL builds "SELECT EXISTS(<query>) AS "exists"".
func (b *Builder) ExistsSQL() (string, []any, error) {
	c, err := b.newCompiler()
	if err != nil {
		return "", nil, err
	}
	c.write("SELECT EXISTS")
	if err := c.subquery(b); err != nil {
		return "", nil, err
	}
	c.write(" AS " + c.g.Quote("exists"))
	return c.result(nil)
}

// Exists reports whether any row matches.
func (b *Builder) Exists(ctx context.Context) (bool, error) {
	db, err := b.bound()
	if err != nil {
		return false, err
	}
	sqlText, args, err := b.ExistsSQL()
	if err != nil {
		return false, err
	}
	var ok bool
	err = db.runSQL(ctx, sqlText, args, func(rows *sql.Rows) error { return rowmap.First(rows, &ok) })
	return ok, err
}

// DoesntExist is the negation of Exists.
func (b *Builder) DoesntExist(ctx context.Context) (bool, error) {
	ok, err := b.Exists(ctx)
	return !ok, err
}

// AggregateSQL builds "SELECT fn(column) AS aggregate FROM ...". Queries with
// group by, having, union, distinct, limit or offset are wrapped as
// "SELECT fn(column) AS aggregate FROM (<query>) AS aggregate_table" so the
// aggregate covers the final row set. Ordering is dropped when it cannot matter.
func (b *Builder) AggregateSQL(fn, column string) (string, []any, error) {
	c, err := b.newCompiler()
	if err != nil {
		return "", nil, err
	}
	if column == "" {
		column = "*"
	}
	wrap := len(b.groups) > 0 || len(b.havings) > 0 || len(b.unions) > 0 || b.distinct ||
		b.limit != nil || b.offset != nil
	inner := b.Clone()
	if !wrap {
		inner.orders = nil
	}
	col := "*"
	if column != "*" {
		if wrap {
			column = bareColumn(column)
		}
		col = c.wrapColumn(column)
	}
	expr := strings.ToUpper(fn) + "(" + col + ") AS " + c.g.Quote("aggregate")
	if !wrap {
		e := Raw(expr)
		inner.columns = []selectItem{{raw: &e}}
		return inner.ToSQL()
	}
	c.write("SELECT " + expr + " FROM ")
	if err := c.subquery(inner); err != nil {
		return "", nil, err
	}
	c.write(" AS " + c.g.Quote("aggregate_table"))
	return c.result(nil)
}

// Aggregate runs fn(column) and scans the result into dest (a pointer to a scalar).
// Min/Max/Sum over no rows yield NULL, which leaves dest at its zero value.
func (b *Builder) Aggregate(ctx context.Context, fn, column string, dest any) error {
	db, err := b.bound()
	if err != nil {
		return err
	}
	sqlText, args, err := b.AggregateSQL(fn, column)
	if err != nil {
		return err
	}
	return db.runSQL(ctx, sqlText, args, func(rows *sql.Rows) error { return rowmap.First(rows, dest) })
}

// Count returns count(*), or count(column) when a column is given.
func (b *Builder) Count(ctx context.Context, column ...string) (int64, error) {
	col := "*"
	if len(column) > 0 && column[0] != "" {
		col = column[0]
	}
	var n int64
	err := b.Aggregate(ctx, "count", col, &n)
	return n, err
}

// Min scans min(column) into dest.
func (b *Builder) Min(ctx context.Context, column string, dest any) error {
	return b.Aggregate(ctx, "min", column, dest)
}

// Max scans max(column) into dest.
func (b *Builder) Max(ctx context.Context, column string, dest any) error {
	return b.Aggregate(ctx, "max", column, dest)
}

// Sum returns sum(column) as float64 (0 when there are no rows).
func (b *Builder) Sum(ctx context.Context, column string) (float64, error) {
	var v float64
	err := b.Aggregate(ctx, "sum", column, &v)
	return v, err
}

// Avg returns avg(column) as float64 (0 when there are no rows).
func (b *Builder) Avg(ctx context.Context, column string) (float64, error) {
	var v float64
	err := b.Aggregate(ctx, "avg", column, &v)
	return v, err
}
