package scan

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/akfaiz/quill/internal/errs"
)

// A minimal database/sql driver that returns one canned result set, so the
// scanner is tested without any real database.
type dataset struct {
	cols []string
	rows [][]driver.Value
}

type connector struct{ ds dataset }

func (c connector) Connect(context.Context) (driver.Conn, error) { return conn(c), nil }
func (c connector) Driver() driver.Driver                        { return fakeDriver{} }

type fakeDriver struct{}

func (fakeDriver) Open(string) (driver.Conn, error) { return nil, errors.New("unused") }

type conn connector

func (conn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("unused") }
func (conn) Close() error                        { return nil }
func (conn) Begin() (driver.Tx, error)           { return nil, errors.New("unused") }
func (c conn) QueryContext(context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return &rows{ds: c.ds}, nil
}

type rows struct {
	ds dataset
	i  int
}

func (r *rows) Columns() []string { return r.ds.cols }
func (r *rows) Close() error      { return nil }
func (r *rows) Next(dest []driver.Value) error {
	if r.i >= len(r.ds.rows) {
		return io.EOF
	}
	copy(dest, r.ds.rows[r.i])
	r.i++
	return nil
}

func result(t *testing.T, cols []string, data ...[]driver.Value) *sql.Rows {
	t.Helper()
	db := sql.OpenDB(connector{dataset{cols, data}})
	t.Cleanup(func() { _ = db.Close() })
	rs, err := db.Query("ignored")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rs.Close() })
	return rs
}

type row = []driver.Value

func TestSnake(t *testing.T) {
	tests := map[string]string{
		"ID": "id", "Name": "name", "UserID": "user_id", "UserName": "user_name",
		"HTTPServer": "http_server", "createdAt": "created_at", "A": "a", "Field2Name": "field2_name",
		"": "",
	}
	for in, want := range tests {
		if got := snake(in); got != want {
			t.Errorf("snake(%q) = %q, want %q", in, got, want)
		}
	}
}

type base struct{ ID int64 }

type user struct {
	base
	UserName string
	Email    *string
	Age      *int
	Bio      string
	Active   bool
	Score    float64
	Joined   time.Time
	Nick     sql.NullString
	Renamed  string `db:"alias"`
	Skipped  string `db:"-"`
	hidden   string //nolint:unused // unexported: must be ignored
}

func TestSliceIntoStructs(t *testing.T) {
	at := time.Date(2024, 3, 7, 9, 5, 2, 0, time.UTC)
	rs := result(t,
		[]string{"id", "user_name", "email", "age", "bio", "active", "score", "joined", "nick", "alias", "unknown_col"},
		row{int64(1), "ann", "a@x", int64(30), nil, true, 1.5, at, "annie", "r1", "ignored"},
		row{int64(2), "bob", nil, nil, "hi", false, 2.5, at, nil, nil, nil},
	)
	var got []user
	if err := Slice(rs, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows", len(got))
	}
	a, b := got[0], got[1]
	if a.ID != 1 || a.UserName != "ann" || a.Email == nil || *a.Email != "a@x" || a.Age == nil || *a.Age != 30 {
		t.Errorf("row 0 wrong: %+v", a)
	}
	if a.Bio != "" { // NULL into a plain string: zero value, not an error
		t.Errorf("NULL string should be empty, got %q", a.Bio)
	}
	if !a.Joined.Equal(at) || a.Nick != (sql.NullString{String: "annie", Valid: true}) || a.Renamed != "r1" {
		t.Errorf("row 0 wrong: %+v", a)
	}
	if b.Email != nil || b.Age != nil || b.Bio != "hi" || b.Active || b.Nick.Valid || b.Renamed != "" {
		t.Errorf("row 1 wrong: %+v", b)
	}
}

func TestSliceIntoPointerElementsAndEmbedding(t *testing.T) {
	rs := result(t, []string{"ID", "USER_NAME"}, row{int64(7), "x"})
	var got []*user // case-insensitive column match, embedded struct promoted
	if err := Slice(rs, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != 7 || got[0].UserName != "x" {
		t.Errorf("got %+v", got)
	}

	type Exported struct{ ID int64 }
	type ptrEmbed struct {
		*Exported
		Name string
	}
	rs = result(t, []string{"id", "name"}, row{int64(9), "n"})
	var pe []ptrEmbed // nil embedded pointer to an exported type is allocated on demand
	if err := Slice(rs, &pe); err != nil {
		t.Fatal(err)
	}
	if pe[0].Exported == nil || pe[0].ID != 9 || pe[0].Name != "n" {
		t.Errorf("embedded pointer not allocated: %+v", pe[0])
	}
}

// An embedded pointer to an unexported type cannot be set through reflect. It
// must be ignored (its columns are discarded), never panic.
func TestEmbeddedPointerToUnexportedTypeIsIgnored(t *testing.T) {
	type hiddenPtr struct {
		*base
		Name string
	}
	rs := result(t, []string{"id", "name"}, row{int64(9), "n"})
	var got []hiddenPtr
	if err := Slice(rs, &got); err != nil {
		t.Fatal(err)
	}
	if got[0].base != nil || got[0].Name != "n" {
		t.Errorf("got %+v", got[0])
	}
}

func TestOuterFieldShadowsEmbedded(t *testing.T) {
	type outer struct {
		base
		ID string
	}
	rs := result(t, []string{"id"}, row{"outer-wins"})
	var got []outer
	if err := Slice(rs, &got); err != nil {
		t.Fatal(err)
	}
	if got[0].ID != "outer-wins" || got[0].base.ID != 0 {
		t.Errorf("got %+v", got[0])
	}
}

func TestSliceIntoMaps(t *testing.T) {
	rs := result(t, []string{"name", "age", "raw"}, row{[]byte("ann"), int64(3), []byte("b")}, row{"bob", nil, nil})
	var got []map[string]any
	if err := Slice(rs, &got); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"name": "ann", "age": int64(3), "raw": "b"}, // []byte arrives as string
		{"name": "bob", "age": nil, "raw": nil},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
}

func TestSliceIntoScalars(t *testing.T) {
	var ints []int
	if err := Slice(result(t, []string{"n"}, row{int64(1)}, row{nil}, row{int64(3)}), &ints); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ints, []int{1, 0, 3}) { // NULL -> 0
		t.Errorf("ints = %v", ints)
	}
	var strs []*string
	if err := Slice(result(t, []string{"s"}, row{"a"}, row{nil}), &strs); err != nil {
		t.Fatal(err)
	}
	if len(strs) != 2 || strs[0] == nil || *strs[0] != "a" || strs[1] != nil {
		t.Errorf("strs = %v", strs)
	}
	var times []time.Time
	at := time.Unix(1700000000, 0).UTC()
	if err := Slice(result(t, []string{"t"}, row{at}), &times); err != nil || !times[0].Equal(at) {
		t.Errorf("times = %v (%v)", times, err)
	}
}

func TestSliceEmptyResultIsNonNil(t *testing.T) {
	var got []user
	if err := Slice(result(t, []string{"id"}), &got); err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Errorf("want empty non-nil slice, got %#v", got)
	}
}

func TestFirst(t *testing.T) {
	cols := []string{"id", "user_name"}
	var u user
	if err := First(result(t, cols, row{int64(1), "a"}, row{int64(2), "b"}), &u); err != nil || u.ID != 1 {
		t.Errorf("struct: %+v %v", u, err)
	}
	var m map[string]any
	if err := First(result(t, cols, row{int64(1), "a"}), &m); err != nil || m["user_name"] != "a" {
		t.Errorf("map: %v %v", m, err)
	}
	var n int
	if err := First(result(t, []string{"n"}, row{int64(5)}), &n); err != nil || n != 5 {
		t.Errorf("scalar: %d %v", n, err)
	}
	n = 99
	if err := First(result(t, []string{"n"}, row{nil}), &n); err != nil || n != 0 {
		t.Errorf("NULL scalar should zero the destination: %d %v", n, err)
	}
	if err := First(result(t, cols), &u); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("empty result: got %v, want sql.ErrNoRows", err)
	}
}

func TestInvalidDestinations(t *testing.T) {
	two := []string{"a", "b"}
	var one user
	var notSlice int
	tests := []struct {
		name string
		call func() error
	}{
		{"Slice non-pointer", func() error { return Slice(result(t, two), []user{}) }},
		{"Slice nil pointer", func() error { return Slice(result(t, two), (*[]user)(nil)) }},
		{"Slice pointer to non-slice", func() error { return Slice(result(t, two), &one) }},
		{"Slice nil", func() error { return Slice(result(t, two), nil) }},
		{"Slice scalar with two columns", func() error { var s []string; return Slice(result(t, two, row{"x", "y"}), &s) }},
		{"First non-pointer", func() error { return First(result(t, two), one) }},
		{"First nil pointer", func() error { return First(result(t, two), (*user)(nil)) }},
		{"First scalar with two columns", func() error { return First(result(t, two, row{int64(1), int64(2)}), &notSlice) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, errs.ErrInvalidArgs) && err == nil {
				t.Errorf("want an error, got nil")
			} else if err != nil && errors.Is(err, sql.ErrNoRows) {
				t.Errorf("unexpected ErrNoRows: %v", err)
			}
		})
	}
	// the input-shape errors are ErrInvalidArgs specifically
	for name, err := range map[string]error{
		"non-pointer": Slice(result(t, two), []user{}),
		"nil":         Slice(result(t, two), nil),
		"not a slice": Slice(result(t, two), &one),
	} {
		if !errors.Is(err, errs.ErrInvalidArgs) {
			t.Errorf("%s: got %v, want ErrInvalidArgs", name, err)
		}
	}
}

func TestScanErrorsPropagate(t *testing.T) {
	// a string that cannot become an int
	var ints []int
	if err := Slice(result(t, []string{"n"}, row{"not a number"}), &ints); err == nil {
		t.Error("conversion error should surface")
	}
}

func TestMapperReusedAcrossRows(t *testing.T) {
	rs := result(t, []string{"id", "user_name"}, row{int64(1), "a"}, row{int64(2), nil})
	cols, _ := rs.Columns()
	m, err := NewMapper(reflect.TypeFor[user](), cols)
	if err != nil {
		t.Fatal(err)
	}
	var seen []user
	for rs.Next() {
		var u user
		if err := m.Scan(rs, reflect.ValueOf(&u).Elem()); err != nil {
			t.Fatal(err)
		}
		seen = append(seen, u)
	}
	if len(seen) != 2 || seen[0].UserName != "a" || seen[1].UserName != "" || seen[1].ID != 2 {
		t.Errorf("seen = %+v", seen)
	}
	if _, err := NewMapper(reflect.TypeFor[int](), []string{"a", "b"}); err == nil {
		t.Error("scalar mapper with two columns should fail")
	}
}

func TestTargetNullGuard(t *testing.T) {
	var s string
	target, post := Target(reflect.ValueOf(&s).Elem())
	if post == nil {
		t.Fatal("plain string needs a post step")
	}
	if _, ok := target.(**string); !ok {
		t.Errorf("target is %T, want **string", target)
	}
	var p *string // pointers handle NULL themselves
	if _, post := Target(reflect.ValueOf(&p).Elem()); post != nil {
		t.Error("pointer should not need a post step")
	}
	var ns sql.NullString // Scanners handle NULL themselves
	if _, post := Target(reflect.ValueOf(&ns).Elem()); post != nil {
		t.Error("sql.Scanner should not need a post step")
	}
}

func TestValueOfColumn(t *testing.T) {
	u := user{base: base{ID: 4}, UserName: "z"}
	tests := []struct {
		name string
		elem reflect.Value
		col  string
		want any
		bad  bool
	}{
		{"struct by snake name", reflect.ValueOf(u), "user_name", "z", false},
		{"struct promoted field", reflect.ValueOf(u), "id", int64(4), false},
		{"struct case-insensitive", reflect.ValueOf(u), "USER_NAME", "z", false},
		{"pointer to struct", reflect.ValueOf(&u), "id", int64(4), false},
		{"map", reflect.ValueOf(map[string]any{"id": 9}), "id", 9, false},
		{"missing in map", reflect.ValueOf(map[string]any{}), "id", nil, true},
		{"missing in struct", reflect.ValueOf(u), "nope", nil, true},
		{"nil pointer", reflect.ValueOf((*user)(nil)), "id", nil, true},
		{"unsupported kind", reflect.ValueOf(5), "id", nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValueOfColumn(tc.elem, tc.col)
			if tc.bad {
				if !errors.Is(err, errs.ErrInvalidArgs) {
					t.Errorf("want ErrInvalidArgs, got %v", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v (%v), want %v", got, err, tc.want)
			}
		})
	}
}

type tagged struct {
	ID       int64   `db:"id,omitempty"`
	Name     string  `db:"name"`
	Email    *string `db:",omitempty"` // empty name: falls back to snake_case
	Secret   string  `db:"-"`
	Created  time.Time
	internal string //nolint:unused // ignored
	base
}

func TestFields(t *testing.T) {
	got := Fields(reflect.TypeFor[tagged]())
	type brief struct {
		Name string
		Omit bool
	}
	var names []brief
	for _, f := range got {
		names = append(names, brief{f.Name, f.OmitEmpty})
	}
	// own fields first (declaration order), then fields promoted from embedded structs
	want := []brief{{"id", true}, {"name", false}, {"email", true}, {"created", false}}
	// base.ID is shadowed by tagged.ID, so only 4 columns remain
	if !reflect.DeepEqual(names, want) {
		t.Errorf("got %+v\nwant %+v", names, want)
	}
}

func TestFieldValue(t *testing.T) {
	email := "a@x"
	v := tagged{ID: 3, Name: "n", Email: &email}
	byName := map[string]Field{}
	for _, f := range Fields(reflect.TypeFor[tagged]()) {
		byName[f.Name] = f
	}
	if fv, ok := byName["id"].Value(reflect.ValueOf(v)); !ok || fv.Int() != 3 {
		t.Errorf("id: %v %v", fv, ok)
	}
	if fv, ok := byName["email"].Value(reflect.ValueOf(v)); !ok || fv.IsNil() {
		t.Errorf("email: %v %v", fv, ok)
	}

	// a field behind a nil embedded pointer has no value
	type Inner struct{ Deep string }
	type holder struct{ *Inner }
	var fields = Fields(reflect.TypeFor[holder]())
	if len(fields) != 1 || fields[0].Name != "deep" {
		t.Fatalf("fields = %+v", fields)
	}
	if _, ok := fields[0].Value(reflect.ValueOf(holder{})); ok {
		t.Error("nil embedded pointer should report ok=false")
	}
	if fv, ok := fields[0].Value(reflect.ValueOf(holder{&Inner{"x"}})); !ok || fv.String() != "x" {
		t.Errorf("deep: %v %v", fv, ok)
	}
}

func TestHasOption(t *testing.T) {
	for opts, want := range map[string]bool{"omitempty": true, "x,omitempty": true, " omitempty ": true, "": false, "omit": false, "omitempty2": false} {
		if got := hasOption(opts, "omitempty"); got != want {
			t.Errorf("hasOption(%q) = %v, want %v", opts, got, want)
		}
	}
}

// ---- struct -> row ----

func TestStructTypeAndValue(t *testing.T) {
	u := user{UserName: "z"}
	if typ, err := StructType(reflect.TypeFor[**user]()); err != nil || typ != reflect.TypeFor[user]() {
		t.Errorf("StructType(**user) = %v, %v", typ, err)
	}
	for name, typ := range map[string]reflect.Type{
		"map": reflect.TypeFor[map[string]any](), "int": reflect.TypeFor[int](), "slice": reflect.TypeFor[[]user](),
	} {
		if _, err := StructType(typ); !errors.Is(err, errs.ErrInvalidArgs) {
			t.Errorf("StructType(%s): got %v, want ErrInvalidArgs", name, err)
		}
	}

	var iface any = &u
	for name, v := range map[string]reflect.Value{
		"struct": reflect.ValueOf(u), "pointer": reflect.ValueOf(&u), "interface": reflect.ValueOf(&iface).Elem(),
	} {
		sv, err := StructValue(v)
		if err != nil || sv.Kind() != reflect.Struct {
			t.Errorf("StructValue(%s) = %v, %v", name, sv, err)
		}
	}
	for name, v := range map[string]reflect.Value{
		"nil pointer": reflect.ValueOf((*user)(nil)), "non-struct": reflect.ValueOf(5), "map": reflect.ValueOf(map[string]any{}),
	} {
		if _, err := StructValue(v); !errors.Is(err, errs.ErrInvalidArgs) {
			t.Errorf("StructValue(%s): got %v, want ErrInvalidArgs", name, err)
		}
	}
}

func TestToRowsOmitEmptyAndPassThrough(t *testing.T) {
	email := "a@x"
	typ := reflect.TypeFor[tagged]()
	rows, err := ToRows(typ, []reflect.Value{reflect.ValueOf(tagged{Name: "n", Email: &email})})
	if err != nil {
		t.Fatal(err)
	}
	// id is omitempty and zero -> left out; email is set -> kept; secret is db:"-"
	if _, has := rows[0]["id"]; has {
		t.Errorf("zero omitempty field was written: %v", rows[0])
	}
	if _, has := rows[0]["secret"]; has {
		t.Errorf("db:\"-\" field was written: %v", rows[0])
	}
	if rows[0]["name"] != "n" || rows[0]["email"] != &email {
		t.Errorf("values should pass through untouched: %v", rows[0])
	}
}

func TestToRowsBatchKeepsAColumnIfAnyRowSetsIt(t *testing.T) {
	typ := reflect.TypeFor[tagged]()
	vals := []reflect.Value{reflect.ValueOf(tagged{Name: "a"}), reflect.ValueOf(tagged{ID: 9, Name: "b"})}
	rows, err := ToRows(typ, vals)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["id"] != int64(0) || rows[1]["id"] != int64(9) {
		t.Errorf("id must be written for every row once any row sets it: %v", rows)
	}
	for i, r := range rows {
		if len(r) != len(rows[0]) {
			t.Errorf("row %d has %d columns, row 0 has %d", i, len(r), len(rows[0]))
		}
	}
}

func TestToRowsNilEmbeddedPointer(t *testing.T) {
	type Inner struct{ Deep string }
	type holder struct {
		*Inner
		Name string
	}
	typ := reflect.TypeFor[holder]()

	// alone, a nil embedded pointer has no value: the column is left out (the database default applies)
	rows, err := ToRows(typ, []reflect.Value{reflect.ValueOf(holder{Name: "n"})})
	if err != nil {
		t.Fatal(err)
	}
	if _, has := rows[0]["deep"]; has {
		t.Errorf("column behind a nil embedded pointer should be omitted: %v", rows[0])
	}

	// in a batch where another row sets it, every row must carry the column: nil becomes NULL
	rows, err = ToRows(typ, []reflect.Value{reflect.ValueOf(holder{Name: "a"}), reflect.ValueOf(holder{Inner: &Inner{"x"}, Name: "b"})})
	if err != nil {
		t.Fatal(err)
	}
	if v, has := rows[0]["deep"]; !has || v != nil || rows[1]["deep"] != "x" {
		t.Errorf("mixed batch: got %v / %v", rows[0], rows[1])
	}
}

func TestToRowsNoColumns(t *testing.T) {
	type none struct {
		X int `db:"-"`
	}
	if _, err := ToRows(reflect.TypeFor[none](), []reflect.Value{reflect.ValueOf(none{})}); !errors.Is(err, errs.ErrInvalidArgs) {
		t.Errorf("got %v, want ErrInvalidArgs", err)
	}
}
