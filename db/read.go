package db

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/akfaiz/quill/internal/rowmap"
)

// run executes the compiled SELECT and hands the rows to fn, always closing them.
func (b *query) run(ctx context.Context, fn func(rows *sql.Rows) error) error {
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
//
// Example:
//
//	var users []User // or []map[string]any, or []string for one column
//	err := conn.Select().From("users").Where("active", true).OrderBy("id").Get(ctx, &users)
//	// SELECT * FROM "users" WHERE "active" = $1 ORDER BY "id" ASC   args: [true]
func (b *query) Get(ctx context.Context, dest any) error {
	return b.run(ctx, func(rows *sql.Rows) error { return rowmap.Slice(rows, dest) })
}

// First scans the first row into dest (*struct, *map[string]any or *scalar).
// It returns ErrNotFound when nothing matches.
//
// Example:
//
//	var u User
//	err := conn.Select().From("users").OrderBy("id").First(ctx, &u)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 1
//	// no row: errors.Is(err, db.ErrNotFound) is true
func (b *query) First(ctx context.Context, dest any) error {
	q := b.withLimit(1)
	return q.run(ctx, func(rows *sql.Rows) error { return rowmap.First(rows, dest) })
}

// withLimit returns a shallow copy of b limited to n rows. Unlike Clone it shares the
// clause slices, which is safe because a copy is only compiled and run, never extended,
// and Limit replaces a pointer instead of writing through it.
func (b *query) withLimit(n int) *query {
	q := *b
	return q.Limit(n)
}

// Find is Where("id", id).First.
//
// Example:
//
//	var u User
//	err := conn.Select().From("users").Find(ctx, 2, &u)
//	// SELECT * FROM "users" WHERE "id" = $1 LIMIT 1   args: [2]
func (b *query) Find(ctx context.Context, id any, dest any) error {
	q := *b
	// clipped, so appending the id predicate copies instead of writing into b's spare capacity
	q.wheres = slices.Clip(isolated(b.wheres))
	return q.Where("id", id).First(ctx, dest)
}

// Sole returns the only matching row: ErrNotFound for none, ErrMultipleRecords for several.
//
// Example:
//
//	var u User
//	err := conn.Select().From("users").Where("name", "ann").Sole(ctx, &u)
//	// SELECT * FROM "users" WHERE "name" = $1 LIMIT 2   args: ["ann"]
//	// two or more rows: err is db.ErrMultipleRecords
func (b *query) Sole(ctx context.Context, dest any) error {
	return b.withLimit(2).run(ctx, func(rows *sql.Rows) error {
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
//
// Example:
//
//	var name string
//	err := conn.Select().From("users").Where("id", 1).Value(ctx, "name", &name)
//	// SELECT "name" FROM "users" WHERE "id" = $1 LIMIT 1   args: [1]
//	// name = "ann"
func (b *query) Value(ctx context.Context, column string, dest any) error {
	return b.Clone().Select(column).Limit(1).run(ctx, func(rows *sql.Rows) error { return rowmap.First(rows, dest) })
}

// Pluck scans one column of every row into dest, a pointer to a slice of scalars.
//
// Example:
//
//	var names []string
//	err := conn.Select().From("users").OrderBy("id").Pluck(ctx, "name", &names)
//	// SELECT "name" FROM "users" ORDER BY "id" ASC
//	// names = ["ann", "bob"]
func (b *query) Pluck(ctx context.Context, column string, dest any) error {
	return b.Clone().Select(column).Get(ctx, dest)
}

// PluckMap scans two columns into dest, a non-nil pointer to a map[K]V (key column -> value column).
//
// Example:
//
//	var byID map[int64]string
//	err := conn.Select().From("users").PluckMap(ctx, "name", "id", &byID)
//	// SELECT "name", "id" FROM "users"
//	// byID = map[1:"ann" 2:"bob"]
func (b *query) PluckMap(ctx context.Context, column, key string, dest any) error {
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
//
// Example:
//
//	sqlText, args, err := conn.Select().From("users").Where("name", "ann").ExistsSQL()
//	// SELECT EXISTS(SELECT * FROM "users" WHERE "name" = $1) AS "exists"   args: ["ann"]
func (b *query) ExistsSQL() (string, []any, error) {
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
//
// Example:
//
//	ok, err := conn.Select().From("users").Where("name", "ann").Exists(ctx)
//	// SELECT EXISTS(SELECT * FROM "users" WHERE "name" = $1) AS "exists"   args: ["ann"]
//	// ok = true
func (b *query) Exists(ctx context.Context) (bool, error) {
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
//
// Example:
//
//	missing, err := conn.Select().From("users").Where("name", "zed").DoesntExist(ctx)
//	// SELECT EXISTS(SELECT * FROM "users" WHERE "name" = $1) AS "exists"   args: ["zed"]
//	// missing = true when no row matches
func (b *query) DoesntExist(ctx context.Context) (bool, error) {
	ok, err := b.Exists(ctx)
	return !ok, err
}

// AggregateSQL builds "SELECT fn(column) AS aggregate FROM ...". Queries with
// group by, having, union, distinct, limit or offset are wrapped as
// "SELECT fn(column) AS aggregate FROM (<query>) AS aggregate_table" so the
// aggregate covers the final row set. Ordering is dropped when it cannot matter.
//
// Example:
//
//	sqlText, args, err := conn.Select().From("orders").Where("status", "paid").AggregateSQL("sum", "total")
//	// SELECT SUM("total") AS "aggregate" FROM "orders" WHERE "status" = $1   args: ["paid"]
func (b *query) AggregateSQL(fn, column string) (string, []any, error) {
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
//
// Example:
//
//	var total int64
//	err := conn.Select().From("orders").Aggregate(ctx, "sum", "total", &total)
//	// SELECT SUM("total") AS "aggregate" FROM "orders"
//	// total = 250
func (b *query) Aggregate(ctx context.Context, fn, column string, dest any) error {
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
//
// Example:
//
//	n, err := conn.Select().From("users").Where("active", true).Count(ctx)
//	// SELECT COUNT(*) AS "aggregate" FROM "users" WHERE "active" = $1   args: [true]
//	// n = 3
//	n, err = conn.Select().From("users").Count(ctx, "email_verified_at")
//	// SELECT COUNT("email_verified_at") AS "aggregate" FROM "users"
func (b *query) Count(ctx context.Context, column ...string) (int64, error) {
	col := "*"
	if len(column) > 0 && column[0] != "" {
		col = column[0]
	}
	var n int64
	err := b.Aggregate(ctx, "count", col, &n)
	return n, err
}

// Min scans min(column) into dest.
//
// Example:
//
//	var lowest int64
//	err := conn.Select().From("orders").Min(ctx, "total", &lowest)
//	// SELECT MIN("total") AS "aggregate" FROM "orders"
//	// lowest = 10
func (b *query) Min(ctx context.Context, column string, dest any) error {
	return b.Aggregate(ctx, "min", column, dest)
}

// Max scans max(column) into dest.
//
// Example:
//
//	var highest int64
//	err := conn.Select().From("orders").Max(ctx, "total", &highest)
//	// SELECT MAX("total") AS "aggregate" FROM "orders"
//	// highest = 90
func (b *query) Max(ctx context.Context, column string, dest any) error {
	return b.Aggregate(ctx, "max", column, dest)
}

// Sum returns sum(column) as float64 (0 when there are no rows).
//
// Example:
//
//	sum, err := conn.Select().From("orders").Sum(ctx, "total")
//	// SELECT SUM("total") AS "aggregate" FROM "orders"
//	// sum = 250.5
func (b *query) Sum(ctx context.Context, column string) (float64, error) {
	var v float64
	err := b.Aggregate(ctx, "sum", column, &v)
	return v, err
}

// Avg returns avg(column) as float64 (0 when there are no rows).
//
// Example:
//
//	avg, err := conn.Select().From("orders").Avg(ctx, "total")
//	// SELECT AVG("total") AS "aggregate" FROM "orders"
//	// avg = 50.1
func (b *query) Avg(ctx context.Context, column string) (float64, error) {
	var v float64
	err := b.Aggregate(ctx, "avg", column, &v)
	return v, err
}
