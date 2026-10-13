package db

import (
	"context"
	"fmt"
)

// UpdateBuilder is a mutable UPDATE statement. Start one with DB.Update, add
// assignments with Set/SetMap/SetRow/Increment/Decrement, narrow it with Where, and
// finish with Exec, Get or ToSQL. Without a WHERE or LIMIT it refuses to build
// (ErrNoWhere) unless AllRows was called. Not safe for concurrent use.
type UpdateBuilder struct {
	q        query
	sets     []setItem
	fromMode bool
}

func (u *UpdateBuilder) set(column string, value any) {
	for k := range u.sets {
		if u.sets[k].column == column {
			u.sets[k].value = value
			return
		}
	}
	u.sets = append(u.sets, setItem{column, value})
}

// Set assigns value to column. Values may be Raw expressions or *SelectBuilder
// subqueries; "col->path" keys update inside a JSON column. Setting the same
// column again replaces the earlier value.
//
// Example:
//
//	conn.Update("users").Set("name", "Ann").Set("age", 31).Where("id", 7)
//	// UPDATE "users" SET "name" = $1, "age" = $2 WHERE "id" = $3   args: [Ann 31 7]
func (u *UpdateBuilder) Set(column string, value any) *UpdateBuilder {
	u.set(column, value)
	return u
}

// SetMap assigns every entry of values, in sorted column order.
//
// Example:
//
//	conn.Update("users").SetMap(map[string]any{"name": "Ann", "age": 31}).Where("id", 7)
//	// UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3   args: [31 Ann 7]
func (u *UpdateBuilder) SetMap(values map[string]any) *UpdateBuilder {
	for _, it := range sortedSets(values) {
		u.set(it.column, it.value)
	}
	return u
}

// SetRow assigns the columns of a struct (see Row for the `db` tags).
//
// Example:
//
//	conn.Update("posts").SetRow(Post{Title: "Hello"}).Where("id", 1)
//	// UPDATE "posts" SET "title" = $1 WHERE "id" = $2   args: [Hello 1]
func (u *UpdateBuilder) SetRow(row any) *UpdateBuilder {
	m, err := Row(row)
	if err != nil {
		u.q.fail(err)
		return u
	}
	return u.SetMap(m)
}

func (u *UpdateBuilder) adjust(op, column string, amount any) *UpdateBuilder {
	if !isNumber(amount) {
		u.q.fail(fmt.Errorf("%w: amount for %q must be numeric, got %T", ErrInvalidArgs, column, amount))
		return u
	}
	u.set(column, adjustment{op, amount})
	return u
}

// Increment sets column = column + amount.
//
// Example:
//
//	conn.Update("posts").Increment("views", 1).Set("seen_by", "ann").Where("id", 7)
//	// UPDATE "posts" SET "views" = "views" + $1, "seen_by" = $2 WHERE "id" = $3   args: [1 ann 7]
func (u *UpdateBuilder) Increment(column string, amount any) *UpdateBuilder {
	return u.adjust("+", column, amount)
}

// Decrement sets column = column - amount.
//
// Example:
//
//	conn.Update("products").Decrement("stock", 2).Where("id", 3)
//	// UPDATE "products" SET "stock" = "stock" - $1 WHERE "id" = $2   args: [2 3]
func (u *UpdateBuilder) Decrement(column string, amount any) *UpdateBuilder {
	return u.adjust("-", column, amount)
}

// IncrementEach increments several columns at once; amounts maps column to amount.
//
// Example:
//
//	conn.Update("stats").IncrementEach(map[string]any{"views": 5, "clicks": 1}).Where("day", "2026-01-01")
//	// UPDATE "stats" SET "clicks" = "clicks" + $1, "views" = "views" + $2 WHERE "day" = $3
//	// args: [1 5 2026-01-01]
func (u *UpdateBuilder) IncrementEach(amounts map[string]any) *UpdateBuilder {
	for _, it := range sortedSets(amounts) {
		u.adjust("+", it.column, it.value)
	}
	return u
}

// DecrementEach decrements several columns at once.
//
// Example:
//
//	conn.Update("stats").DecrementEach(map[string]any{"views": 5, "clicks": 1}).Where("day", "2026-01-01")
//	// UPDATE "stats" SET "clicks" = "clicks" - $1, "views" = "views" - $2 WHERE "day" = $3
//	// args: [1 5 2026-01-01]
func (u *UpdateBuilder) DecrementEach(amounts map[string]any) *UpdateBuilder {
	for _, it := range sortedSets(amounts) {
		u.adjust("-", it.column, it.value)
	}
	return u
}

// FromJoins switches to the Postgres "UPDATE t SET ... FROM j1, j2 WHERE ..." form,
// where each Join's ON conditions become part of the WHERE. Other dialects return
// ErrUnsupported (MySQL supports Join directly).
//
// Example:
//
//	conn.Update("posts").Join("users", "users.id", "=", "posts.user_id").FromJoins().
//		Set("hidden", true).Where("users.banned", true)
//	// UPDATE "posts" SET "hidden" = $1 FROM "users"
//	//   WHERE "users"."banned" = $2 AND "users"."id" = "posts"."user_id"   args: [true true]
func (u *UpdateBuilder) FromJoins() *UpdateBuilder { u.fromMode = true; return u }

// Clone returns an independent copy. Cloning a nil *UpdateBuilder returns nil.
//
// Example:
//
//	base := conn.Update("users").Set("active", false)
//	one := base.Clone().Where("id", 1)
func (u *UpdateBuilder) Clone() *UpdateBuilder {
	if u == nil {
		return nil
	}
	n := *u
	n.q = *u.q.Clone()
	n.sets = append([]setItem(nil), u.sets...)
	return &n
}

// ToSQL compiles the UPDATE into SQL text and its ordered bind arguments. No
// assignments returns ErrNoValues; no WHERE or LIMIT returns ErrNoWhere unless
// AllRows was called. Joins use native UPDATE ... JOIN on MySQL and a ctid/rowid
// subquery on Postgres/SQLite.
//
// Example:
//
//	sqlText, args, err := conn.Update("users").Set("name", "Ann").Where("id", 7).ToSQL()
//	// UPDATE "users" SET "name" = $1 WHERE "id" = $2   args: [Ann 7]
func (u *UpdateBuilder) ToSQL() (string, []any, error) {
	if u.fromMode {
		return u.q.updateFromSQL(u.sets)
	}
	return u.q.updateSQL(u.sets)
}

// Exec runs the update and returns the rows affected.
//
// Example:
//
//	n, err := conn.Update("users").Set("name", "Ann").Where("id", 7).Exec(ctx)
//	// UPDATE "users" SET "name" = $1 WHERE "id" = $2   args: [Ann 7]
func (u *UpdateBuilder) Exec(ctx context.Context) (int64, error) {
	return u.q.execSQL(ctx, u.ToSQL)
}

// Get runs the update and scans the RETURNING rows into dest. Call Returning first.
//
// Example:
//
//	var rows []User
//	err := conn.Update("users").Set("name", "Ann").Where("id", 7).Returning("*").Get(ctx, &rows)
//	// UPDATE "users" SET "name" = $1 WHERE "id" = $2 RETURNING *   args: [Ann 7]
func (u *UpdateBuilder) Get(ctx context.Context, dest any) error {
	return u.q.returningInto(ctx, dest, u.ToSQL)
}
