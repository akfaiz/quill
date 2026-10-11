package db

import "context"

// DeleteBuilder is a mutable DELETE statement. Start one with DB.Delete, narrow it
// with Where, and finish with Exec, Get or ToSQL. Without a WHERE or LIMIT it
// refuses to build (ErrNoWhere) unless AllRows was called. Not safe for concurrent use.
type DeleteBuilder struct{ q query }

// Clone returns an independent copy. Cloning a nil *DeleteBuilder returns nil.
//
// Example:
//
//	base := conn.Delete("sessions").Where("expired", true)
//	old := base.Clone().Where("created_at", "<", cutoff)
func (d *DeleteBuilder) Clone() *DeleteBuilder {
	if d == nil {
		return nil
	}
	return &DeleteBuilder{q: *d.q.Clone()}
}

// ToSQL compiles the DELETE into SQL text and its ordered bind arguments. Joins use
// "DELETE alias FROM ... JOIN" on MySQL and a ctid/rowid subquery on Postgres/SQLite.
//
// Example:
//
//	sqlText, args, err := conn.Delete("users").Where("id", 7).ToSQL()
//	// DELETE FROM "users" WHERE "id" = $1   args: [7]
func (d *DeleteBuilder) ToSQL() (string, []any, error) { return d.q.deleteSQL() }

// Exec runs the delete and returns the rows affected.
//
// Example:
//
//	n, err := conn.Delete("users").Where("id", 7).Exec(ctx)
//	// DELETE FROM "users" WHERE "id" = $1   args: [7]
func (d *DeleteBuilder) Exec(ctx context.Context) (int64, error) {
	return d.q.execSQL(ctx, d.ToSQL)
}

// Get runs the delete and scans the RETURNING rows into dest. Call Returning first.
//
// Example:
//
//	var gone []User
//	err := conn.Delete("users").Where("id", 7).Returning("*").Get(ctx, &gone)
//	// DELETE FROM "users" WHERE "id" = $1 RETURNING *   args: [7]
func (d *DeleteBuilder) Get(ctx context.Context, dest any) error {
	return d.q.returningInto(ctx, dest, d.ToSQL)
}
