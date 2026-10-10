// Package rowmap maps database/sql rows onto structs, maps and scalars.
package rowmap

import (
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/akfaiz/quill/internal/errs"
)

var (
	scannerType = reflect.TypeFor[sql.Scanner]()
	timeType    = reflect.TypeFor[time.Time]()
	anyType     = reflect.TypeFor[any]()
)

type fieldInfo struct {
	index     []int
	typ       reflect.Type
	omitEmpty bool
}

type structMap struct {
	exact map[string]fieldInfo
	fold  map[string]fieldInfo
	order []string // column names in discovery order (own fields, then embedded)
}

var structCache sync.Map // reflect.Type -> *structMap

func getStructMap(t reflect.Type) *structMap {
	if v, ok := structCache.Load(t); ok {
		return v.(*structMap)
	}
	sm := &structMap{exact: map[string]fieldInfo{}, fold: map[string]fieldInfo{}}
	collectFields(t, nil, sm)
	v, _ := structCache.LoadOrStore(t, sm)
	return v.(*structMap)
}

// collectFields maps column names to fields: `db:"name"` tag, else snake_case of
// the field name. `db:"-"` skips; the `omitempty` option marks a field to be left
// out of writes while it holds its zero value. Embedded structs are flattened; an
// outer field shadows an embedded one of the same name.
func collectFields(t reflect.Type, prefix []int, sm *structMap) {
	var embedded []reflect.StructField
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("db")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && !isScalarStruct(f.Type) {
				// an embedded pointer to an unexported type cannot be allocated
				// through reflect, so its fields are not scannable: skip, don't panic
				if f.Type.Kind() != reflect.Pointer || f.IsExported() {
					embedded = append(embedded, f)
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = snake(f.Name)
		}
		idx := append(append([]int(nil), prefix...), i)
		if _, taken := sm.exact[name]; !taken {
			fi := fieldInfo{index: idx, typ: f.Type, omitEmpty: hasOption(opts, "omitempty")}
			sm.exact[name] = fi
			sm.order = append(sm.order, name)
			if _, ok := sm.fold[strings.ToLower(name)]; !ok {
				sm.fold[strings.ToLower(name)] = fi
			}
		}
	}
	for _, f := range embedded {
		ft := f.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		collectFields(ft, append(append([]int(nil), prefix...), f.Index...), sm)
	}
}

func hasOption(opts, want string) bool {
	for o := range strings.SplitSeq(opts, ",") {
		if strings.TrimSpace(o) == want {
			return true
		}
	}
	return false
}

// Field is one writable column of a struct.
type Field struct {
	Name      string
	OmitEmpty bool
	index     []int
}

// Fields lists the columns of struct type t (a struct, not a pointer) in
// discovery order, using the same tag rules as scanning.
func Fields(t reflect.Type) []Field {
	sm := getStructMap(t)
	out := make([]Field, len(sm.order))
	for i, name := range sm.order {
		fi := sm.exact[name]
		out[i] = Field{Name: name, OmitEmpty: fi.omitEmpty, index: fi.index}
	}
	return out
}

// Value reads the field from struct value v. ok is false when the field sits
// behind a nil embedded pointer, so there is no value to read.
func (f Field) Value(v reflect.Value) (val reflect.Value, ok bool) {
	for i, x := range f.index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v, true
}

// isScalarStruct reports struct types that scan as one value (time.Time, sql.Null*, Scanners).
func isScalarStruct(t reflect.Type) bool {
	return t == timeType || reflect.PointerTo(t).Implements(scannerType) || t.Implements(scannerType)
}

func snake(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if unicode.IsUpper(r) {
			if i > 0 && (unicode.IsLower(rs[i-1]) || unicode.IsDigit(rs[i-1]) ||
				(unicode.IsUpper(rs[i-1]) && i+1 < len(rs) && unicode.IsLower(rs[i+1]))) {
				b.WriteByte('_')
			}
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

type elemKind int

const (
	kindStruct elemKind = iota
	kindMap
	kindScalar
)

func kindOf(t reflect.Type) elemKind {
	switch {
	case t.Kind() == reflect.Struct && !isScalarStruct(t):
		return kindStruct
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String && t.Elem() == anyType:
		return kindMap
	}
	return kindScalar
}

// Mapper scans successive rows into values of one element type.
type Mapper struct {
	typ  reflect.Type
	kind elemKind
	cols []string
	// kindStruct: per column, the target field (nil index = discard).
	fields []*fieldInfo
}

func NewMapper(t reflect.Type, cols []string) (*Mapper, error) {
	m := &Mapper{typ: t, kind: kindOf(t), cols: cols}
	switch m.kind {
	case kindStruct:
		sm := getStructMap(t)
		m.fields = make([]*fieldInfo, len(cols))
		for i, c := range cols {
			if fi, ok := sm.exact[c]; ok {
				m.fields[i] = &fi
			} else if fi, ok := sm.fold[strings.ToLower(c)]; ok {
				m.fields[i] = &fi
			}
		}
	case kindScalar:
		if len(cols) != 1 {
			return nil, fmt.Errorf("quill: scanning into %s needs exactly one column, got %d", t, len(cols))
		}
	}
	return m, nil
}

// fieldByIndexAlloc walks an index path, allocating nil embedded pointers.
func fieldByIndexAlloc(v reflect.Value, index []int) reflect.Value {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v
}

// Scan scans the current row into dst (addressable, of m.typ). NULL in a
// plain (non-pointer, non-Scanner) field leaves the zero value instead of failing.
func (m *Mapper) Scan(rows *sql.Rows, dst reflect.Value) error {
	switch m.kind {
	case kindMap:
		vals := make([]any, len(m.cols))
		ptrs := make([]any, len(m.cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if dst.IsNil() {
			dst.Set(reflect.MakeMapWithSize(m.typ, len(m.cols)))
		}
		for i, c := range m.cols {
			if b, ok := vals[i].([]byte); ok {
				vals[i] = string(b)
			}
			dst.SetMapIndex(reflect.ValueOf(c), reflect.ValueOf(&vals[i]).Elem())
		}
		return nil
	case kindScalar:
		return scanScalar(rows, dst)
	}
	targets := make([]any, len(m.cols))
	var posts []func()
	for i, fi := range m.fields {
		if fi == nil {
			targets[i] = new(any)
			continue
		}
		t, post := Target(fieldByIndexAlloc(dst, fi.index))
		targets[i] = t
		if post != nil {
			posts = append(posts, post)
		}
	}
	if err := rows.Scan(targets...); err != nil {
		return err
	}
	for _, p := range posts {
		p()
	}
	return nil
}

func scanScalar(rows *sql.Rows, dst reflect.Value) error {
	t, post := Target(dst)
	if err := rows.Scan(t); err != nil {
		return err
	}
	if post != nil {
		post()
	}
	return nil
}

// Target returns what to hand rows.Scan for dst, plus an optional step to
// run afterwards. Plain value types are scanned through a **T so that NULL
// leaves the zero value rather than failing.
func Target(dst reflect.Value) (any, func()) {
	if !needsNullGuard(dst.Type()) {
		return dst.Addr().Interface(), nil
	}
	tmp := reflect.New(reflect.PointerTo(dst.Type()))
	return tmp.Interface(), func() {
		if p := tmp.Elem(); !p.IsNil() {
			dst.Set(p.Elem())
		} else {
			dst.SetZero()
		}
	}
}

// needsNullGuard: plain value types that cannot hold NULL themselves.
func needsNullGuard(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map:
		return false
	case reflect.Slice:
		return t.Elem().Kind() != reflect.Uint8 // []byte takes NULL as nil
	}
	return !reflect.PointerTo(t).Implements(scannerType)
}

// Slice scans every row into *[]T (T: struct, *struct, map[string]any or scalar).
func Slice(rows *sql.Rows, dest any) error {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Slice {
		return fmt.Errorf("%w: destination must be a non-nil pointer to a slice, got %T", errs.ErrInvalidArgs, dest)
	}
	slice := rv.Elem()
	elemT := slice.Type().Elem()
	isPtr := elemT.Kind() == reflect.Pointer && kindOf(elemT.Elem()) != kindScalar
	baseT := elemT
	if isPtr {
		baseT = elemT.Elem()
	}
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	m, err := NewMapper(baseT, cols)
	if err != nil {
		return err
	}
	out := reflect.MakeSlice(slice.Type(), 0, 8)
	for rows.Next() {
		item := reflect.New(baseT).Elem()
		if err := m.Scan(rows, item); err != nil {
			return err
		}
		if isPtr {
			p := reflect.New(baseT)
			p.Elem().Set(item)
			item = p
		}
		out = reflect.Append(out, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	slice.Set(out)
	return nil
}

// First scans the first row into dest (*struct, *map[string]any or *scalar);
// it returns sql.ErrNoRows when there are no rows. The remaining rows are ignored.
func First(rows *sql.Rows, dest any) error {
	rv := reflect.ValueOf(dest)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("%w: destination must be a non-nil pointer, got %T", errs.ErrInvalidArgs, dest)
	}
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	target := rv.Elem()
	m, err := NewMapper(target.Type(), cols)
	if err != nil {
		return err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if m.kind == kindMap {
		target.SetZero()
	}
	return m.Scan(rows, target)
}

// ValueOfColumn reads a column from a scanned element (struct, *struct or map).
func ValueOfColumn(elem reflect.Value, column string) (any, error) {
	for elem.Kind() == reflect.Pointer {
		if elem.IsNil() {
			return nil, fmt.Errorf("%w: nil row", errs.ErrInvalidArgs)
		}
		elem = elem.Elem()
	}
	switch elem.Kind() {
	case reflect.Map:
		v := elem.MapIndex(reflect.ValueOf(column))
		if !v.IsValid() {
			return nil, fmt.Errorf("%w: column %q is not in the result", errs.ErrInvalidArgs, column)
		}
		return v.Interface(), nil
	case reflect.Struct:
		sm := getStructMap(elem.Type())
		fi, ok := sm.exact[column]
		if !ok {
			fi, ok = sm.fold[strings.ToLower(column)]
		}
		if !ok {
			return nil, fmt.Errorf("%w: column %q has no field in %s", errs.ErrInvalidArgs, column, elem.Type())
		}
		return fieldByIndexAlloc(elem, fi.index).Interface(), nil
	}
	return nil, fmt.Errorf("%w: cannot read column %q from %s", errs.ErrInvalidArgs, column, elem.Type())
}

// ---- struct -> row (the write side of the same tag rules) ----

// StructType dereferences pointer types and requires a struct.
func StructType(t reflect.Type) (reflect.Type, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: expected a struct or pointer to struct, got %s", errs.ErrInvalidArgs, t)
	}
	return t, nil
}

func StructValue(v reflect.Value) (reflect.Value, error) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return v, fmt.Errorf("%w: nil %s", errs.ErrInvalidArgs, v.Type())
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return v, fmt.Errorf("%w: expected a struct, got %s", errs.ErrInvalidArgs, v.Type())
	}
	return v, nil
}

func ToRows(t reflect.Type, vals []reflect.Value) ([]map[string]any, error) {
	fields := Fields(t)
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: %s has no columns (exported fields or db tags)", errs.ErrInvalidArgs, t)
	}
	keep := make([]bool, len(fields))
	for _, v := range vals {
		for i, f := range fields {
			if fv, ok := f.Value(v); ok && (!f.OmitEmpty || !fv.IsZero()) {
				keep[i] = true
			}
		}
	}
	rows := make([]map[string]any, len(vals))
	for r, v := range vals {
		m := make(map[string]any, len(fields))
		for i, f := range fields {
			if !keep[i] {
				continue
			}
			if fv, ok := f.Value(v); ok {
				m[f.Name] = fv.Interface()
			} else {
				m[f.Name] = nil // behind a nil embedded pointer: NULL
			}
		}
		rows[r] = m
	}
	return rows, nil
}
