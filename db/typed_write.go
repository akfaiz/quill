package db

import "context"

// ---- writes: structs are converted with Row/Rows (see Row for the tag rules) ----

// Insert inserts structs and returns the rows affected. No rows is a no-op.
//
// Example:
//
//	n, err := db.From[User](conn).Insert(ctx, User{Name: "ann", Age: 30}, User{Name: "bob", Age: 25})
//	// INSERT INTO "users" ("age", "name") VALUES ($1, $2), ($3, $4)   args: [30 ann 25 bob]
//	// n: 2 (the omitempty ID is zero, so it is left out)
func (q *Query[T]) Insert(ctx context.Context, rows ...T) (int64, error) {
	maps, err := Rows(rows)
	if err != nil {
		return 0, err
	}
	return q.b.Insert(ctx, maps...)
}

// InsertOrIgnore inserts structs, skipping any that conflict.
//
// Example:
//
//	n, err := db.From[User](conn).InsertOrIgnore(ctx, User{ID: 1, Name: "ann"})
//	// INSERT INTO "users" ("id", "name") VALUES ($1, $2) ON CONFLICT DO NOTHING   args: [1 ann]
//	// n: 0 when the row already existed
func (q *Query[T]) InsertOrIgnore(ctx context.Context, rows ...T) (int64, error) {
	maps, err := Rows(rows)
	if err != nil {
		return 0, err
	}
	return q.b.InsertOrIgnore(ctx, maps...)
}

// InsertGetID inserts one struct and returns its integer id (see Builder.InsertGetID).
//
// Example:
//
//	id, err := db.From[User](conn).InsertGetID(ctx, User{Name: "cy", Age: 41})
//	// INSERT INTO "users" ("age", "name") VALUES ($1, $2) RETURNING "id"   args: [41 cy]
//	// id: 3
func (q *Query[T]) InsertGetID(ctx context.Context, row T, sequence ...string) (int64, error) {
	m, err := Row(row)
	if err != nil {
		return 0, err
	}
	return q.b.InsertGetID(ctx, m, sequence...)
}

// Upsert inserts structs, updating the update columns (nil = every inserted
// column) on conflict with uniqueBy. MySQL ignores uniqueBy.
//
// Example:
//
//	rows := []User{{ID: 1, Name: "ann", Age: 31}, {ID: 2, Name: "bob", Age: 26}}
//	n, err := db.From[User](conn).Upsert(ctx, rows, []string{"id"}, []string{"age"})
//	// INSERT INTO "users" ("age", "id", "name") VALUES ($1, $2, $3), ($4, $5, $6)
//	//   ON CONFLICT ("id") DO UPDATE SET "age" = "excluded"."age"   args: [31 1 ann 26 2 bob]
//	// n: 2
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
//
// Example:
//
//	n, err := db.From[User](conn).Where("id", 1).Update(ctx, User{Name: "ann", Age: 31})
//	// UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3   args: [31 ann 1]
//	// n: 1; without Where (or AllRows) err is "update/delete without a where clause"
func (q *Query[T]) Update(ctx context.Context, row T) (int64, error) {
	m, err := Row(row)
	if err != nil {
		return 0, err
	}
	return q.b.Update(ctx, m)
}

// UpdateMap is Update with an explicit column map, for zero values and raw expressions.
//
// Example:
//
//	n, err := db.From[User](conn).Where("id", 1).UpdateMap(ctx,
//		map[string]any{"age": 0, "name": db.Raw("upper(name)")})
//	// UPDATE "users" SET "age" = $1, "name" = upper(name) WHERE "id" = $2   args: [0 1]
//	// n: 1
func (q *Query[T]) UpdateMap(ctx context.Context, values map[string]any) (int64, error) {
	return q.b.Update(ctx, values)
}

// Delete removes the matching rows. It refuses to run without a where clause
// unless AllRows() was applied.
//
// Example:
//
//	n, err := db.From[User](conn).Where("id", 2).Delete(ctx)
//	// DELETE FROM "users" WHERE "id" = $1   args: [2]
//	// n: 1
func (q *Query[T]) Delete(ctx context.Context) (int64, error) { return q.b.Delete(ctx) }

// Truncate empties the table and resets its identity.
//
// Example:
//
//	err := db.From[User](conn).Truncate(ctx)
//	// TRUNCATE "users" RESTART IDENTITY   args: []
func (q *Query[T]) Truncate(ctx context.Context) error { return q.b.Truncate(ctx) }

// ---- aggregates ----

// Count returns the number of matching rows.
//
// Example:
//
//	n, err := db.From[User](conn).Where("age", ">", 18).Count(ctx)
//	// SELECT COUNT(*) AS "aggregate" FROM "users" WHERE "age" > $1   args: [18]
//	// n: 3
func (q *Query[T]) Count(ctx context.Context) (int64, error) { return q.b.Count(ctx) }

// Exists reports whether any row matches.
//
// Example:
//
//	ok, err := db.From[User](conn).Where("name", "ann").Exists(ctx)
//	// SELECT EXISTS(SELECT * FROM "users" WHERE "name" = $1) AS "exists"   args: [ann]
//	// ok: true
func (q *Query[T]) Exists(ctx context.Context) (bool, error) { return q.b.Exists(ctx) }

// DoesntExist reports whether no row matches.
//
// Example:
//
//	none, err := db.From[User](conn).Where("name", "ann").DoesntExist(ctx)
//	// SELECT EXISTS(SELECT * FROM "users" WHERE "name" = $1) AS "exists"   args: [ann]
//	// none: false when a matching row exists
func (q *Query[T]) DoesntExist(ctx context.Context) (bool, error) { return q.b.DoesntExist(ctx) }

// Sum returns sum(column) as float64 (0 when there are no rows).
//
// Example:
//
//	total, err := db.From[User](conn).Sum(ctx, "age")
//	// SELECT SUM("age") AS "aggregate" FROM "users"   args: []
//	// total: 86
func (q *Query[T]) Sum(ctx context.Context, column string) (float64, error) {
	return q.b.Sum(ctx, column)
}

// Avg returns avg(column) as float64 (0 when there are no rows).
//
// Example:
//
//	avg, err := db.From[User](conn).Avg(ctx, "age")
//	// SELECT AVG("age") AS "aggregate" FROM "users"   args: []
//	// avg: 28.7
func (q *Query[T]) Avg(ctx context.Context, column string) (float64, error) {
	return q.b.Avg(ctx, column)
}
