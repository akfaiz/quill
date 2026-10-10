package db

import (
	"context"
	"database/sql"
	"fmt"
	"iter"
	"reflect"

	"github.com/akfaiz/quill/internal/rowmap"
)

// Chunk reads size rows at a time into dest (a pointer to a slice, refilled each
// round) and calls fn; return false to stop early. The query must have an
// ORDER BY (ErrNoOrder otherwise). It pages by offset, so rows changed by fn
// can be skipped or repeated: prefer ChunkByID when fn updates the filtered column.
//
// Example:
//
//	var batch []User
//	err := conn.Table("users").OrderBy("id").Chunk(ctx, 2, &batch, func() (bool, error) {
//		return true, nil // false stops early
//	})
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 2 OFFSET 0
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 2 OFFSET 2   (until a short batch)
func (b *Builder) Chunk(ctx context.Context, size int, dest any, fn func() (bool, error)) error {
	if _, err := checkPaging(1, size); err != nil {
		return err
	}
	if len(b.orders) == 0 && len(b.unionOrders) == 0 {
		return ErrNoOrder
	}
	for page := 1; ; page++ {
		if err := b.Clone().ForPage(page, size).Get(ctx, dest); err != nil {
			return err
		}
		n := sliceLen(dest)
		if n == 0 {
			return nil
		}
		if more, err := fn(); err != nil || !more {
			return err
		}
		if n < size {
			return nil
		}
	}
}

// ChunkByID is Chunk using keyset paging on a unique column (default "id"), so
// it is safe while fn updates or deletes the rows it was given.
//
// Example:
//
//	var batch []User
//	err := conn.Table("users").ChunkByID(ctx, 2, &batch, func() (bool, error) {
//		return true, nil
//	})
//	// SELECT * FROM "users" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2
//	// SELECT * FROM "users" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 2   args: [2]
//	// another key column: ChunkByID(ctx, 5, &batch, fn, "order_no")
func (b *Builder) ChunkByID(ctx context.Context, size int, dest any, fn func() (bool, error), column ...string) error {
	if _, err := checkPaging(1, size); err != nil {
		return err
	}
	col := idColumn(column)
	var last any
	for {
		if err := b.Clone().ForPageAfterID(size, last, col).Get(ctx, dest); err != nil {
			return err
		}
		n := sliceLen(dest)
		if n == 0 {
			return nil
		}
		if more, err := fn(); err != nil || !more {
			return err
		}
		if n < size {
			return nil
		}
		v, err := rowmap.ValueOfColumn(reflect.ValueOf(dest).Elem().Index(n-1), bareColumn(col))
		if err != nil {
			return err
		}
		if isNilValue(v) {
			return fmt.Errorf("%w: ChunkByID column %q is NULL", ErrInvalidArgs, col)
		}
		last = v
	}
}

// isNilValue reports whether v is nil or a nil pointer (a NULL *T field).
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// Each streams rows one at a time into dest (a pointer to a struct, map or
// scalar, reset before every row) and calls fn, holding a single row in memory.
//
// Example:
//
//	var u User
//	err := conn.Table("users").OrderBy("id").Each(ctx, &u, func() error {
//		fmt.Println(u.ID, u.Name) // u is refilled for every row
//		return nil
//	})
//	// SELECT * FROM "users" ORDER BY "id" ASC
func (b *Builder) Each(ctx context.Context, dest any, fn func() error) error {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("%w: Each destination must be a non-nil pointer, got %T", ErrInvalidArgs, dest)
	}
	target := rv.Elem()
	return b.run(ctx, func(rows *sql.Rows) error {
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		m, err := rowmap.NewMapper(target.Type(), cols)
		if err != nil {
			return err
		}
		for rows.Next() {
			target.SetZero()
			if err := m.Scan(rows, target); err != nil {
				return err
			}
			if err := fn(); err != nil {
				return err
			}
		}
		return rows.Err()
	})
}

// Stream yields each row as T (struct, map[string]any or scalar) while holding one
// row in memory. Stop early by breaking out of the loop; the cursor is then closed.
// An error ends the sequence after being yielded once.
//
// Example:
//
//	q := conn.Table("users").OrderBy("id")
//	for u, err := range db.Stream[User](ctx, q) {
//		if err != nil {
//			break
//		}
//		fmt.Println(u.ID, u.Name)
//	}
//	// SELECT * FROM "users" ORDER BY "id" ASC
//	// scalar rows: db.Stream[string](ctx, conn.Table("users").Select("name"))
func Stream[T any](ctx context.Context, b *Builder) iter.Seq2[T, error] {
	return func(yield func(T, error) bool) {
		var zero T
		stopped := false
		var cur T
		err := b.Each(ctx, &cur, func() error {
			if !yield(cur, nil) {
				stopped = true
				return errStopStream
			}
			return nil
		})
		if stopped {
			return
		}
		if err != nil {
			yield(zero, err)
		}
	}
}

var errStopStream = fmt.Errorf("quill: stream stopped")
