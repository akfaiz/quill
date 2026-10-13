package db

import (
	"fmt"
	"reflect"

	"github.com/akfaiz/quill/internal/rowmap"
)

// Row converts a struct (or non-nil pointer to one) into a column map for the
// write methods, using the same `db` tags as scanning:
//
//	`db:"name"`         column name (default: snake_case of the field name)
//	`db:"name,omitempty"` skipped while the field holds its zero value
//	`db:"-"`            never written
//
// Embedded structs are flattened and unexported fields are ignored. Fields behind a
// nil embedded pointer are treated like omitted zero values. Values are
// passed through untouched, so pointers (nil -> NULL), sql.Null* types and Raw
// expressions all work. A field that is omitted for being zero cannot be set to
// its zero value through a struct: use a pointer field or a map for that.
//
// Example:
//
//	type Post struct {
//		ID    int64  `db:"id,omitempty"`
//		Title string `db:"title"`
//		Notes string `db:"-"`
//	}
//	row, err := db.Row(Post{Title: "Hello", Notes: "internal"})
//	// row = map[title:Hello]   (zero ID omitted, Notes never written)
func Row(v any) (map[string]any, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil row", ErrInvalidArgs)
	}
	rv := reflect.ValueOf(v)
	t, err := rowmap.StructType(rv.Type())
	if err != nil {
		return nil, err
	}
	sv, err := rowmap.StructValue(rv)
	if err != nil {
		return nil, err
	}
	rows, err := rowmap.ToRows(t, []reflect.Value{sv})
	if err != nil {
		return nil, err
	}
	return rows[0], nil
}
