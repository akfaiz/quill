package db

import (
	"fmt"
	"reflect"

	"github.com/akfaiz/quill/internal/scan"
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
func Row(v any) (map[string]any, error) {
	if v == nil {
		return nil, fmt.Errorf("%w: nil row", ErrInvalidArgs)
	}
	rv := reflect.ValueOf(v)
	t, err := scan.StructType(rv.Type())
	if err != nil {
		return nil, err
	}
	sv, err := scan.StructValue(rv)
	if err != nil {
		return nil, err
	}
	rows, err := scan.ToRows(t, []reflect.Value{sv})
	if err != nil {
		return nil, err
	}
	return rows[0], nil
}

// Rows converts structs into column maps for a batch insert. Every row gets
// the same columns: an `omitempty` column is left out only when it is zero in
// every row, since a batch insert cannot skip a column for just some rows.
func Rows[T any](vs []T) ([]map[string]any, error) {
	if len(vs) == 0 {
		return nil, nil
	}
	t, err := scan.StructType(reflect.TypeFor[T]())
	if err != nil {
		return nil, err
	}
	vals := make([]reflect.Value, len(vs))
	for i := range vs {
		if vals[i], err = scan.StructValue(reflect.ValueOf(&vs[i]).Elem()); err != nil {
			return nil, fmt.Errorf("row %d: %w", i, err)
		}
	}
	return scan.ToRows(t, vals)
}
