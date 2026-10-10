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
func (b *Builder) execSQL(ctx context.Context, build func() (string, []any, error)) (int64, error) {
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

// noop turns "nothing to write" into a successful no-op,.
func noop(n int64, err error) (int64, error) {
	if errors.Is(err, ErrNoValues) {
		return 0, nil
	}
	return n, err
}

// Insert inserts rows and returns the rows affected. No rows is a no-op.
//
// Example:
//
//	n, err := conn.Table("users").Insert(ctx,
//		map[string]any{"name": "Ann"}, map[string]any{"name": "Bob"})
//	// INSERT INTO "users" ("name") VALUES ($1), ($2)   args: [Ann Bob]   (n = 2 rows affected)
func (b *Builder) Insert(ctx context.Context, rows ...map[string]any) (int64, error) {
	return noop(b.execSQL(ctx, func() (string, []any, error) { return b.InsertSQL(rows...) }))
}

// InsertOrIgnore inserts rows, skipping any that conflict.
//
// Example:
//
//	n, err := conn.Table("users").InsertOrIgnore(ctx, map[string]any{"email": "ann@example.com"})
//	// INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT DO NOTHING
//	// (n = 0 if the row existed)
func (b *Builder) InsertOrIgnore(ctx context.Context, rows ...map[string]any) (int64, error) {
	return noop(b.execSQL(ctx, func() (string, []any, error) { return b.InsertOrIgnoreSQL(rows...) }))
}

// InsertUsing runs "INSERT INTO t (columns) SELECT ...".
//
// Example:
//
//	sub := conn.Table("users").Select("name").Where("active", false)
//	n, err := conn.Table("archived_users").InsertUsing(ctx, []string{"name"}, sub)
//	// INSERT INTO "archived_users" ("name") SELECT "name" FROM "users" WHERE "active" = $1
//	// args: [false]   (n = rows inserted)
func (b *Builder) InsertUsing(ctx context.Context, columns []string, sub *Builder) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.InsertUsingSQL(columns, sub) })
}

// InsertOrIgnoreUsing is InsertUsing that skips conflicting rows.
//
// Example:
//
//	sub := conn.Table("users").Select("name")
//	n, err := conn.Table("archived_users").InsertOrIgnoreUsing(ctx, []string{"name"}, sub)
//	// INSERT INTO "archived_users" ("name") SELECT "name" FROM "users" ON CONFLICT DO NOTHING
func (b *Builder) InsertOrIgnoreUsing(ctx context.Context, columns []string, sub *Builder) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.InsertOrIgnoreUsingSQL(columns, sub) })
}

// InsertGetID inserts one row and returns its integer id: via RETURNING on
// Postgres (column sequence, default "id"), via LastInsertId on MySQL/SQLite.
//
// Example:
//
//	id, err := conn.Table("users").InsertGetID(ctx, map[string]any{"name": "Ann"})
//	// Postgres: INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"   (id = 42)
//	// MySQL:    INSERT INTO `users` (`name`) VALUES (?)   (id from LastInsertId)
func (b *Builder) InsertGetID(ctx context.Context, row map[string]any, sequence ...string) (int64, error) {
	db, err := b.bound()
	if err != nil {
		return 0, err
	}
	sqlText, args, err := b.InsertGetIDSQL(row, sequence...)
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

// Upsert inserts rows, updating the update columns (nil = all) on conflict with
// uniqueBy. MySQL ignores uniqueBy and reports 2 affected rows per updated row.
//
// Example:
//
//	rows := []map[string]any{{"email": "ann@example.com", "name": "Ann"}}
//	n, err := conn.Table("users").Upsert(ctx, rows, []string{"email"}, []string{"name"})
//	// INSERT INTO "users" ("email", "name") VALUES ($1, $2)
//	//   ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"
//	// args: [ann@example.com Ann]
func (b *Builder) Upsert(ctx context.Context, rows []map[string]any, uniqueBy, update []string) (int64, error) {
	return noop(b.execSQL(ctx, func() (string, []any, error) { return b.UpsertSQL(rows, uniqueBy, update) }))
}

// Update sets columns on the matching rows and returns the rows affected.
// It refuses to run without a where clause unless AllRows() was called.
//
// Example:
//
//	n, err := conn.Table("users").Where("id", 7).Update(ctx, map[string]any{"name": "Ann"})
//	// UPDATE "users" SET "name" = $1 WHERE "id" = $2   args: [Ann 7]   (n = rows affected)
//	// Without Where (and no AllRows) it returns ErrNoWhere and sends nothing.
func (b *Builder) Update(ctx context.Context, values map[string]any) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.UpdateSQL(values) })
}

// UpdateFrom runs UpdateFromSQL (Postgres).
//
// Example:
//
//	n, err := conn.Table("posts").Join("users", "users.id", "=", "posts.user_id").
//		Where("users.banned", true).UpdateFrom(ctx, map[string]any{"hidden": true})
//	// UPDATE "posts" SET "hidden" = $1 FROM "users"
//	//   WHERE "users"."banned" = $2 AND "users"."id" = "posts"."user_id"   args: [true true]
func (b *Builder) UpdateFrom(ctx context.Context, values map[string]any) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.UpdateFromSQL(values) })
}

// UpdateOrInsert updates the first row matching attributes with values, or
// inserts attributes+values when none matches. It reports whether the table
// changed hands (always true unless values is empty and a row exists).
// Not atomic: wrap in Transaction and add a unique index if races matter.
//
// Example:
//
//	ok, err := conn.Table("users").UpdateOrInsert(ctx,
//		map[string]any{"email": "ann@example.com"}, map[string]any{"name": "Ann"})
//	// SELECT EXISTS(SELECT * FROM "users" WHERE "email" = $1) AS "exists"
//	// args: [ann@example.com]
//	// no match: INSERT INTO "users" ("email", "name") VALUES ($1, $2)   (ok = true)
func (b *Builder) UpdateOrInsert(ctx context.Context, attributes, values map[string]any) (bool, error) {
	q := b.Clone()
	keys := make([]string, 0, len(attributes))
	for k := range attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		q.Where(k, attributes[k])
	}
	exists, err := q.Exists(ctx)
	if err != nil {
		return false, err
	}
	if !exists {
		merged := make(map[string]any, len(attributes)+len(values))
		maps.Copy(merged, attributes)
		maps.Copy(merged, values)
		_, insErr := b.Clone().Insert(ctx, merged)
		return insErr == nil, insErr
	}
	if len(values) == 0 {
		return true, nil
	}
	_, err = q.Limit(1).Update(ctx, values)
	return err == nil, err
}

// Increment adds amount to column (extra columns are set too) on matching rows.
//
// Example:
//
//	n, err := conn.Table("posts").Where("id", 7).Increment(ctx, "views", 1, nil)
//	// UPDATE "posts" SET "views" = "views" + $1 WHERE "id" = $2
//	// args: [1 7]   (n = rows affected)
func (b *Builder) Increment(ctx context.Context, column string, amount any, extra map[string]any) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.IncrementSQL(column, amount, extra) })
}

// Decrement subtracts amount from column on matching rows.
//
// Example:
//
//	n, err := conn.Table("products").Where("id", 3).
//		Decrement(ctx, "stock", 2, map[string]any{"sold_by": "ann"})
//	// UPDATE "products" SET "stock" = "stock" - $1, "sold_by" = $2 WHERE "id" = $3
//	// args: [2 ann 3]
func (b *Builder) Decrement(ctx context.Context, column string, amount any, extra map[string]any) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.DecrementSQL(column, amount, extra) })
}

// IncrementEach increments several columns at once.
//
// Example:
//
//	n, err := conn.Table("stats").Where("day", "2026-01-01").
//		IncrementEach(ctx, map[string]any{"views": 5, "clicks": 1}, nil)
//	// UPDATE "stats" SET "clicks" = "clicks" + $1, "views" = "views" + $2 WHERE "day" = $3
//	// args: [1 5 2026-01-01]
func (b *Builder) IncrementEach(ctx context.Context, amounts, extra map[string]any) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.IncrementEachSQL(amounts, extra) })
}

// DecrementEach decrements several columns at once.
//
// Example:
//
//	n, err := conn.Table("stats").Where("day", "2026-01-01").
//		DecrementEach(ctx, map[string]any{"views": 5, "clicks": 1}, nil)
//	// UPDATE "stats" SET "clicks" = "clicks" - $1, "views" = "views" - $2 WHERE "day" = $3
//	// args: [1 5 2026-01-01]
func (b *Builder) DecrementEach(ctx context.Context, amounts, extra map[string]any) (int64, error) {
	return b.execSQL(ctx, func() (string, []any, error) { return b.DecrementEachSQL(amounts, extra) })
}

// Delete removes the matching rows and returns the rows affected. It refuses to
// run without a where clause unless AllRows() was called; use Truncate to empty a table.
//
// Example:
//
//	n, err := conn.Table("users").Where("id", 7).Delete(ctx)
//	// DELETE FROM "users" WHERE "id" = $1   args: [7]   (n = rows affected)
//	n, err = conn.Table("sessions").AllRows().Delete(ctx) // DELETE FROM "sessions"
func (b *Builder) Delete(ctx context.Context) (int64, error) {
	return b.execSQL(ctx, b.DeleteSQL)
}

// Truncate empties the table and resets its identity (see TruncateSQL).
//
// Example:
//
//	err := conn.Table("users").Truncate(ctx)
//	// Postgres: TRUNCATE "users" RESTART IDENTITY
//	// MySQL:    TRUNCATE TABLE `users`
func (b *Builder) Truncate(ctx context.Context) error {
	db, err := b.bound()
	if err != nil {
		return err
	}
	stmts, err := b.TruncateSQL()
	if err != nil {
		return err
	}
	for _, s := range stmts {
		if _, err := db.exec(ctx, s.SQL, s.Args); err != nil {
			return err
		}
	}
	return nil
}

// ---- RETURNING ----

// returningInto runs a write built with Returning(...) and scans the returned rows into dest.
func (b *Builder) returningInto(ctx context.Context, dest any, build func() (string, []any, error)) error {
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

// InsertReturning inserts rows and scans the Returning(...) columns into dest
// (a pointer to a slice). Postgres and SQLite only.
//
// Example:
//
//	var out []struct {
//		ID   int64  `db:"id"`
//		Name string `db:"name"`
//	}
//	err := conn.Table("users").Returning("id", "name").
//		InsertReturning(ctx, &out, map[string]any{"name": "Ann"})
//	// INSERT INTO "users" ("name") VALUES ($1) RETURNING "id", "name"   (out = [{1 Ann}])
func (b *Builder) InsertReturning(ctx context.Context, dest any, rows ...map[string]any) error {
	return b.returningInto(ctx, dest, func() (string, []any, error) { return b.InsertSQL(rows...) })
}

// UpdateReturning updates matching rows and scans the Returning(...) columns into dest.
//
// Example:
//
//	err := conn.Table("users").Where("id", 7).Returning("id", "name").
//		UpdateReturning(ctx, &out, map[string]any{"name": "Ann"})
//	// UPDATE "users" SET "name" = $1 WHERE "id" = $2 RETURNING "id", "name"   args: [Ann 7]
func (b *Builder) UpdateReturning(ctx context.Context, dest any, values map[string]any) error {
	return b.returningInto(ctx, dest, func() (string, []any, error) { return b.UpdateSQL(values) })
}

// DeleteReturning deletes matching rows and scans the Returning(...) columns into dest.
//
// Example:
//
//	err := conn.Table("users").Where("active", false).Returning("id").
//		DeleteReturning(ctx, &out)
//	// DELETE FROM "users" WHERE "active" = $1 RETURNING "id"   args: [false]   (out = [{7} {8}])
func (b *Builder) DeleteReturning(ctx context.Context, dest any) error {
	return b.returningInto(ctx, dest, b.DeleteSQL)
}
