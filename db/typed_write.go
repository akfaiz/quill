package db

import "context"

// ---- writes: structs are converted with Row/Rows (see Row for the tag rules) ----

// Insert inserts structs and returns the rows affected. No rows is a no-op.
func (q *Query[T]) Insert(ctx context.Context, rows ...T) (int64, error) {
	maps, err := Rows(rows)
	if err != nil {
		return 0, err
	}
	return q.b.Insert(ctx, maps...)
}

// InsertOrIgnore inserts structs, skipping any that conflict.
func (q *Query[T]) InsertOrIgnore(ctx context.Context, rows ...T) (int64, error) {
	maps, err := Rows(rows)
	if err != nil {
		return 0, err
	}
	return q.b.InsertOrIgnore(ctx, maps...)
}

// InsertGetID inserts one struct and returns its integer id (see Builder.InsertGetID).
func (q *Query[T]) InsertGetID(ctx context.Context, row T, sequence ...string) (int64, error) {
	m, err := Row(row)
	if err != nil {
		return 0, err
	}
	return q.b.InsertGetID(ctx, m, sequence...)
}

// Upsert inserts structs, updating the update columns (nil = every inserted
// column) on conflict with uniqueBy. MySQL ignores uniqueBy.
func (q *Query[T]) Upsert(ctx context.Context, rows []T, uniqueBy, update []string) (int64, error) {
	maps, err := Rows(rows)
	if err != nil {
		return 0, err
	}
	return q.b.Upsert(ctx, maps, uniqueBy, update)
}

// Update sets the struct's non-omitted columns on the matching rows. It refuses to
// run without a where clause unless AllRows() was applied, and returns
// ErrNoValues when every column was omitted.
func (q *Query[T]) Update(ctx context.Context, row T) (int64, error) {
	m, err := Row(row)
	if err != nil {
		return 0, err
	}
	return q.b.Update(ctx, m)
}

// UpdateMap is Update with an explicit column map, for zero values and raw expressions.
func (q *Query[T]) UpdateMap(ctx context.Context, values map[string]any) (int64, error) {
	return q.b.Update(ctx, values)
}

// Delete removes the matching rows. It refuses to run without a where clause
// unless AllRows() was applied.
func (q *Query[T]) Delete(ctx context.Context) (int64, error) { return q.b.Delete(ctx) }

// Truncate empties the table and resets its identity.
func (q *Query[T]) Truncate(ctx context.Context) error { return q.b.Truncate(ctx) }

// ---- aggregates ----

// Count returns the number of matching rows.
func (q *Query[T]) Count(ctx context.Context) (int64, error) { return q.b.Count(ctx) }

// Exists reports whether any row matches.
func (q *Query[T]) Exists(ctx context.Context) (bool, error) { return q.b.Exists(ctx) }

// DoesntExist reports whether no row matches.
func (q *Query[T]) DoesntExist(ctx context.Context) (bool, error) { return q.b.DoesntExist(ctx) }

// Sum returns sum(column) as float64 (0 when there are no rows).
func (q *Query[T]) Sum(ctx context.Context, column string) (float64, error) {
	return q.b.Sum(ctx, column)
}

// Avg returns avg(column) as float64 (0 when there are no rows).
func (q *Query[T]) Avg(ctx context.Context, column string) (float64, error) {
	return q.b.Avg(ctx, column)
}
