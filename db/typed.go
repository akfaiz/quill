package db

import (
	"context"
	"iter"
)

// Query reads rows of type T from a Builder. Go methods cannot take type
// parameters, so Builder cannot return []T itself; wrap the finished builder
// once instead:
//
//	users, err := db.As[User](conn.Table("users").Where("active", true)).Get(ctx)
//
// T may be a struct (`db:"col"` tags, else snake_case field names), a
// map[string]any, or a scalar when the query selects a single column.
type Query[T any] struct{ b *Builder }

// As wraps b for typed reads. The builder is not copied: keep chaining on it
// before calling As, or use Builder to get it back.
//
// Example:
//
//	q := db.As[string](conn.Table("users").Select("name"))
//	names, err := q.Get(ctx)
//	// SELECT "name" FROM "users"   args: []
//	// names: [ann bob]
func As[T any](b *Builder) *Query[T] { return &Query[T]{b: b} }

// Builder returns the underlying builder for further chaining.
//
// Example:
//
//	sql, args, err := db.From[User](conn).Where("age", ">", 18).Builder().OrderBy("name").ToSQL()
//	// SELECT * FROM "users" WHERE "age" > $1 ORDER BY "name" ASC   args: [18]
func (q *Query[T]) Builder() *Builder { return q.b }

// Get returns every matching row.
//
// Example (User has TableName() returning "users"; conn is a *db.DB):
//
//	users, err := db.From[User](conn).OrderBy("id").Get(ctx)
//	// SELECT * FROM "users" ORDER BY "id" ASC   args: []
//	// users: [{1 ann 30} {2 bob 25}]
func (q *Query[T]) Get(ctx context.Context) ([]T, error) {
	var out []T
	err := q.b.Get(ctx, &out)
	return out, err
}

// First returns the first row, or ErrNotFound.
//
// Example:
//
//	u, err := db.From[User](conn).Where("name", "ann").First(ctx)
//	// SELECT * FROM "users" WHERE "name" = $1 LIMIT 1   args: [ann]
//	// u.Name: ann; no row: errors.Is(err, db.ErrNotFound)
func (q *Query[T]) First(ctx context.Context) (T, error) {
	var out T
	err := q.b.First(ctx, &out)
	return out, err
}

// Find returns the row whose "id" equals id, or ErrNotFound.
//
// Example:
//
//	u, err := db.From[User](conn).Find(ctx, 2)
//	// SELECT * FROM "users" WHERE "id" = $1 LIMIT 1   args: [2]
//	// u.Name: bob
func (q *Query[T]) Find(ctx context.Context, id any) (T, error) {
	var out T
	err := q.b.Find(ctx, id, &out)
	return out, err
}

// Sole returns the only matching row: ErrNotFound for none, ErrMultipleRecords for several.
//
// Example:
//
//	u, err := db.From[User](conn).Where("name", "ann").Sole(ctx)
//	// SELECT * FROM "users" WHERE "name" = $1 LIMIT 2   args: [ann]
//	// u.Name: ann; two rows: err is ErrMultipleRecords
func (q *Query[T]) Sole(ctx context.Context) (T, error) {
	var out T
	err := q.b.Sole(ctx, &out)
	return out, err
}

// Paged is a length-aware page with its rows.
type Paged[T any] struct {
	Page
	Items []T
}

// SimplePaged is a count-free page with its rows.
type SimplePaged[T any] struct {
	SimplePage
	Items []T
}

// CursorPaged is a keyset page with its rows.
type CursorPaged[T any] struct {
	CursorPage
	Items []T
}

// Paginate counts the rows, then returns the requested 1-based page.
//
// Example:
//
//	p, err := db.From[User](conn).OrderBy("id").Paginate(ctx, 2, 2)
//	// SELECT COUNT(*) AS "aggregate" FROM "users"   args: []
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 2 OFFSET 2   args: []
//	// p.Items: [{3 cy 41}], p.Total: 3, p.CurrentPage: 2, p.LastPage: 2
func (q *Query[T]) Paginate(ctx context.Context, page, perPage int) (Paged[T], error) {
	var items []T
	p, err := q.b.Paginate(ctx, page, perPage, &items)
	return Paged[T]{Page: p, Items: items}, err
}

// SimplePaginate returns a page and whether another follows, without a count query.
//
// Example:
//
//	p, err := db.From[User](conn).OrderBy("id").SimplePaginate(ctx, 1, 2)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 3 OFFSET 0   args: []
//	// len(p.Items): 2, p.HasMore: true
func (q *Query[T]) SimplePaginate(ctx context.Context, page, perPage int) (SimplePaged[T], error) {
	var items []T
	p, err := q.b.SimplePaginate(ctx, page, perPage, &items)
	return SimplePaged[T]{SimplePage: p, Items: items}, err
}

// CursorPaginate pages forward by keyset; see Builder.CursorPaginate.
//
// Example:
//
//	p, err := db.From[User](conn).CursorPaginate(ctx, 2, "")
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 3   args: []
//	// len(p.Items): 2, p.HasMore: true; pass p.NextCursor to fetch the next page
func (q *Query[T]) CursorPaginate(
	ctx context.Context,
	perPage int,
	cursor string,
	opt ...CursorOptions,
) (CursorPaged[T], error) {
	var items []T
	p, err := q.b.CursorPaginate(ctx, perPage, cursor, &items, opt...)
	return CursorPaged[T]{CursorPage: p, Items: items}, err
}

// Chunk reads size rows at a time and calls fn; return false to stop. The query
// needs an ORDER BY. See Builder.Chunk.
//
// Example:
//
//	err := db.From[User](conn).OrderBy("id").Chunk(ctx, 2, func(batch []User) (bool, error) {
//		// batch of 2, then batch of 1; return false to stop
//		return true, nil
//	})
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 2 OFFSET 0   args: []
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 2 OFFSET 2   args: []
func (q *Query[T]) Chunk(ctx context.Context, size int, fn func([]T) (bool, error)) error {
	var rows []T
	return q.b.Chunk(ctx, size, &rows, func() (bool, error) { return fn(rows) })
}

// ChunkByID is Chunk with keyset paging on a unique column (default "id").
//
// Example:
//
//	err := db.From[User](conn).ChunkByID(ctx, 2, func(batch []User) (bool, error) {
//		// batch of 2 (last id 2), then batch of 1 (last id 3)
//		return true, nil
//	})
//	// SELECT * FROM "users" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2   args: []
//	// SELECT * FROM "users" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 2   args: [2]
func (q *Query[T]) ChunkByID(ctx context.Context, size int, fn func([]T) (bool, error), column ...string) error {
	var rows []T
	return q.b.ChunkByID(ctx, size, &rows, func() (bool, error) { return fn(rows) }, column...)
}

// Each streams rows to fn one at a time, holding a single row in memory.
//
// Example:
//
//	err := db.From[User](conn).Each(ctx, func(u User) error {
//		fmt.Println(u.ID, u.Name) // 1 ann, then 2 bob
//		return nil
//	})
//	// SELECT * FROM "users"   args: []
func (q *Query[T]) Each(ctx context.Context, fn func(T) error) error {
	var cur T
	return q.b.Each(ctx, &cur, func() error { return fn(cur) })
}

// Stream yields rows one at a time; break out of the loop to stop early.
//
// Example:
//
//	for u, err := range db.From[User](conn).Stream(ctx) {
//		if err != nil {
//			break
//		}
//		fmt.Println(u.Name) // ann, then bob
//	}
//	// SELECT * FROM "users"   args: []
func (q *Query[T]) Stream(ctx context.Context) iter.Seq2[T, error] { return Stream[T](ctx, q.b) }

// returningBuilder clones the builder and defaults RETURNING to every column.
func (q *Query[T]) returningBuilder() *Builder {
	b := q.b.Clone()
	if len(b.returning) == 0 {
		b.returning = []string{"*"}
	}
	return b
}

// InsertReturning inserts the structs and returns the rows the database wrote
// (the Returning(...) columns, default all). PostgreSQL and SQLite only.
//
// Example:
//
//	rows, err := db.From[User](conn).InsertReturning(ctx, User{Name: "dee", Age: 22})
//	// INSERT INTO "users" ("age", "name") VALUES ($1, $2) RETURNING *   args: [22 dee]
//	// rows: [{7 dee 22}]
func (q *Query[T]) InsertReturning(ctx context.Context, rows ...T) ([]T, error) {
	maps, err := Rows(rows)
	if err != nil {
		return nil, err
	}
	var out []T
	err = q.returningBuilder().InsertReturning(ctx, &out, maps...)
	return out, err
}

// UpdateReturning updates the matching rows from the struct's non-omitted
// columns and returns the updated rows.
//
// Example:
//
//	q := db.From[User](conn).Where("id", 1)
//	rows, err := q.UpdateReturning(ctx, User{Name: "ann", Age: 31})
//	// UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3 RETURNING *   args: [31 ann 1]
//	// rows: [{1 ann 31}]
func (q *Query[T]) UpdateReturning(ctx context.Context, row T) ([]T, error) {
	m, err := Row(row)
	if err != nil {
		return nil, err
	}
	var out []T
	err = q.returningBuilder().UpdateReturning(ctx, &out, m)
	return out, err
}

// DeleteReturning deletes the matching rows and returns them.
//
// Example:
//
//	rows, err := db.From[User](conn).Where("id", 2).DeleteReturning(ctx)
//	// DELETE FROM "users" WHERE "id" = $1 RETURNING *   args: [2]
//	// rows: [{2 bob 25}]
func (q *Query[T]) DeleteReturning(ctx context.Context) ([]T, error) {
	var out []T
	err := q.returningBuilder().DeleteReturning(ctx, &out)
	return out, err
}

// ---- generic scalar helpers ----

// ValueAs returns one column of the first row as V (the zero value for NULL), or ErrNotFound.
//
// Example:
//
//	name, err := db.ValueAs[string](ctx, conn.Table("users").Where("id", 1), "name")
//	// SELECT "name" FROM "users" WHERE "id" = $1 LIMIT 1   args: [1]
//	// name: ann
func ValueAs[V any](ctx context.Context, b *Builder, column string) (V, error) {
	var v V
	err := b.Value(ctx, column, &v)
	return v, err
}

// PluckAs returns one column of every row as []V.
//
// Example:
//
//	ids, err := db.PluckAs[int64](ctx, conn.Table("users").OrderBy("id"), "id")
//	// SELECT "id" FROM "users" ORDER BY "id" ASC   args: []
//	// ids: [1 2]
func PluckAs[V any](ctx context.Context, b *Builder, column string) ([]V, error) {
	var out []V
	err := b.Pluck(ctx, column, &out)
	return out, err
}

// AggregateAs runs fn(column) and returns it as V. It is the typed form of
// Aggregate: MinAs/MaxAs on a timestamp column return time.Time, for example.
//
// Example:
//
//	avg, err := db.AggregateAs[float64](ctx, conn.Table("orders"), "avg", "total")
//	// SELECT AVG("total") AS "aggregate" FROM "orders"   args: []
//	// avg: 3.5
func AggregateAs[V any](ctx context.Context, b *Builder, fn, column string) (V, error) {
	var v V
	err := b.Aggregate(ctx, fn, column, &v)
	return v, err
}

// MinAs returns min(column) as V.
//
// Example:
//
//	t, err := db.MinAs[time.Time](ctx, conn.Table("users"), "created_at")
//	// SELECT MIN("created_at") AS "aggregate" FROM "users"   args: []
//	// t: 2026-01-02 03:04:05 UTC
func MinAs[V any](ctx context.Context, b *Builder, column string) (V, error) {
	return AggregateAs[V](ctx, b, "min", column)
}

// MaxAs returns max(column) as V.
//
// Example:
//
//	oldest, err := db.MaxAs[int](ctx, conn.Table("users"), "age")
//	// SELECT MAX("age") AS "aggregate" FROM "users"   args: []
//	// oldest: 41
func MaxAs[V any](ctx context.Context, b *Builder, column string) (V, error) {
	return AggregateAs[V](ctx, b, "max", column)
}
