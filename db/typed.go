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
func As[T any](b *Builder) *Query[T] { return &Query[T]{b: b} }

// Builder returns the underlying builder for further chaining.
func (q *Query[T]) Builder() *Builder { return q.b }

// Get returns every matching row.
func (q *Query[T]) Get(ctx context.Context) ([]T, error) {
	var out []T
	err := q.b.Get(ctx, &out)
	return out, err
}

// First returns the first row, or ErrNotFound.
func (q *Query[T]) First(ctx context.Context) (T, error) {
	var out T
	err := q.b.First(ctx, &out)
	return out, err
}

// Find returns the row whose "id" equals id, or ErrNotFound.
func (q *Query[T]) Find(ctx context.Context, id any) (T, error) {
	var out T
	err := q.b.Find(ctx, id, &out)
	return out, err
}

// Sole returns the only matching row: ErrNotFound for none, ErrMultipleRecords for several.
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
func (q *Query[T]) Paginate(ctx context.Context, page, perPage int) (Paged[T], error) {
	var items []T
	p, err := q.b.Paginate(ctx, page, perPage, &items)
	return Paged[T]{Page: p, Items: items}, err
}

// SimplePaginate returns a page and whether another follows, without a count query.
func (q *Query[T]) SimplePaginate(ctx context.Context, page, perPage int) (SimplePaged[T], error) {
	var items []T
	p, err := q.b.SimplePaginate(ctx, page, perPage, &items)
	return SimplePaged[T]{SimplePage: p, Items: items}, err
}

// CursorPaginate pages forward by keyset; see Builder.CursorPaginate.
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
func (q *Query[T]) Chunk(ctx context.Context, size int, fn func([]T) (bool, error)) error {
	var rows []T
	return q.b.Chunk(ctx, size, &rows, func() (bool, error) { return fn(rows) })
}

// ChunkByID is Chunk with keyset paging on a unique column (default "id").
func (q *Query[T]) ChunkByID(ctx context.Context, size int, fn func([]T) (bool, error), column ...string) error {
	var rows []T
	return q.b.ChunkByID(ctx, size, &rows, func() (bool, error) { return fn(rows) }, column...)
}

// Each streams rows to fn one at a time, holding a single row in memory.
func (q *Query[T]) Each(ctx context.Context, fn func(T) error) error {
	var cur T
	return q.b.Each(ctx, &cur, func() error { return fn(cur) })
}

// Stream yields rows one at a time; break out of the loop to stop early.
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
func (q *Query[T]) DeleteReturning(ctx context.Context) ([]T, error) {
	var out []T
	err := q.returningBuilder().DeleteReturning(ctx, &out)
	return out, err
}

// ---- generic scalar helpers ----

// ValueAs returns one column of the first row as V (the zero value for NULL), or ErrNotFound.
func ValueAs[V any](ctx context.Context, b *Builder, column string) (V, error) {
	var v V
	err := b.Value(ctx, column, &v)
	return v, err
}

// PluckAs returns one column of every row as []V.
func PluckAs[V any](ctx context.Context, b *Builder, column string) ([]V, error) {
	var out []V
	err := b.Pluck(ctx, column, &out)
	return out, err
}

// AggregateAs runs fn(column) and returns it as V. It is the typed form of
// Aggregate: MinAs/MaxAs on a timestamp column return time.Time, for example.
func AggregateAs[V any](ctx context.Context, b *Builder, fn, column string) (V, error) {
	var v V
	err := b.Aggregate(ctx, fn, column, &v)
	return v, err
}

// MinAs returns min(column) as V.
func MinAs[V any](ctx context.Context, b *Builder, column string) (V, error) {
	return AggregateAs[V](ctx, b, "min", column)
}

// MaxAs returns max(column) as V.
func MaxAs[V any](ctx context.Context, b *Builder, column string) (V, error) {
	return AggregateAs[V](ctx, b, "max", column)
}
