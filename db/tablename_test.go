package db_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

type valueTable struct{ ID int64 }

func (valueTable) TableName() string { return "value_rows" }

type pointerTable struct{ ID int64 }

func (*pointerTable) TableName() string { return "pointer_rows" }

type embedsTable struct {
	valueTable
	Name string
}

type overridesTable struct{ valueTable }

func (overridesTable) TableName() string { return "overridden" }

type noTable struct{ ID int64 }

// derefTable's method dereferences a nil embedded pointer on a zero value.
type tableHolder struct{ Table string }

type derefTable struct{ *tableHolder }

func (d derefTable) TableName() string { return d.Table }

// countedTable counts how often its TableName is evaluated.
var countedCalls int

type countedTable struct{}

func (countedTable) TableName() string { countedCalls++; return "counted" }

type emptyTable struct{}

func (emptyTable) TableName() string { return "" }

func TestTableName(t *testing.T) {
	tests := []struct {
		name string
		got  func() (string, error)
		want string
	}{
		{"value receiver", db.TableName[valueTable], "value_rows"},
		{"pointer receiver", db.TableName[pointerTable], "pointer_rows"},
		{"pointer type parameter", db.TableName[*valueTable], "value_rows"},
		{"promoted through embedding", db.TableName[embedsTable], "value_rows"},
		{"outer method wins over the embedded one", db.TableName[overridesTable], "overridden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.got()
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
			// second call is served from the cache and must agree
			if again, _ := tc.got(); again != got {
				t.Errorf("cached value differs: %q vs %q", again, got)
			}
		})
	}
	for name, get := range map[string]func() (string, error){
		"no method":     db.TableName[noTable],
		"empty name":    db.TableName[emptyTable],
		"map":           db.TableName[map[string]any],
		"scalar":        db.TableName[int64],
		"pointer, none": db.TableName[*noTable],
	} {
		t.Run("error: "+name, func(t *testing.T) {
			if _, err := get(); !errors.Is(err, db.ErrInvalidArgs) {
				t.Errorf("got %v, want ErrInvalidArgs", err)
			}
		})
	}
	if _, err := db.TableName[noTable](); !strings.Contains(err.Error(), "noTable") {
		t.Errorf("the error should name the type: %v", err)
	}
}

// A TableName that panics on a zero value is an error, not a crash in the caller.
func TestTableNamePanicBecomesAnError(t *testing.T) {
	_, err := db.TableName[derefTable]()
	require.ErrorIs(t, err, db.ErrInvalidArgs)
	for _, want := range []string{"derefTable", "panicked", "zero value"} {
		assert.Contains(t, err.Error(), want)
	}
	// the same failure surfaces through From, at the terminal call
	q := db.From[derefTable](db.Use(db.Postgres)).Where("id", 1)
	if _, _, err := q.Builder().ToSQL(); !errors.Is(err, db.ErrInvalidArgs) {
		t.Errorf("From: got %v", err)
	}
	// a failure is not cached as a success
	if _, err := db.TableName[derefTable](); err == nil {
		t.Error("second call should fail the same way")
	}
}

// TableName runs once per type; later calls come from the cache.
func TestTableNameIsEvaluatedOncePerType(t *testing.T) {
	countedCalls = 0
	for range 5 {
		if name, err := db.TableName[countedTable](); err != nil || name != "counted" {
			t.Fatalf("got %q, %v", name, err)
		}
	}
	db.From[countedTable](db.Use(db.Postgres))
	assert.Equal(t, 1, countedCalls)
}

func TestFromUsesTheTypesTable(t *testing.T) {
	conn := db.Use(db.Postgres) // building SQL never touches the connection
	sql, args, err := db.From[valueTable](conn).Where("id", 7).Builder().ToSQL()
	require.NoError(t, err)
	if want := `SELECT * FROM "value_rows" WHERE "id" = $1`; sql != want || !reflect.DeepEqual(args, []any{7}) {
		t.Errorf("got %s %v", sql, args)
	}

	// the database prefix applies, like any table from conn.Table
	prefixed := db.Use(db.MySQL, db.WithPrefix("app_"))
	sql, _, _ = db.From[pointerTable](prefixed).Builder().ToSQL()
	if want := "SELECT * FROM `app_pointer_rows`"; sql != want {
		t.Errorf("got %s want %s", sql, want)
	}
}

func TestFromReportsProblemsAtTheTerminalCall(t *testing.T) {
	conn := db.Use(db.Postgres)
	// the chain keeps working; the error surfaces when the query is used
	q := db.From[noTable](conn).Where("id", 1).OrderBy("id")
	if _, _, err := q.Builder().ToSQL(); !errors.Is(err, db.ErrInvalidArgs) {
		t.Errorf("no TableName: got %v", err)
	}
	if q.Builder().Err() == nil {
		t.Error("Err should expose the problem")
	}
	if _, _, err := db.From[valueTable](nil).Builder().ToSQL(); !errors.Is(err, db.ErrNotBound) {
		t.Errorf("nil conn: got %v", err)
	}
}
