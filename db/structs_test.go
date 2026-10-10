package db_test

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/akfaiz/quill/db"
)

type account struct {
	ID      int64   `db:"id,omitempty"`
	Name    string  `db:"name"`
	Email   *string `db:"email"`
	Bio     string  `db:",omitempty"`
	Secret  string  `db:"-"`
	Nick    sql.NullString
	Created time.Time `db:"created_at,omitempty"`
	hidden  string    //nolint:unused // must be ignored
}

type audit struct{ By string }

type withEmbedded struct {
	audit
	Title string
}

func str(s string) *string { return &s }

func TestRow(t *testing.T) {
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		name string
		in   any
		want map[string]any
	}{
		{"omitempty skips zero, nil pointer stays NULL", account{Name: "Ann"},
			map[string]any{"name": "Ann", "email": (*string)(nil), "nick": sql.NullString{}}},
		{"non-zero omitempty fields are kept", account{ID: 7, Name: "Ann", Email: str("a@x"), Bio: "hi", Created: at},
			map[string]any{"id": int64(7), "name": "Ann", "email": str("a@x"), "bio": "hi", "created_at": at, "nick": sql.NullString{}}},
		{"pointer to struct", &account{Name: "Bob"},
			map[string]any{"name": "Bob", "email": (*string)(nil), "nick": sql.NullString{}}},
		{"embedded struct is flattened", withEmbedded{audit{"me"}, "t"},
			map[string]any{"title": "t", "by": "me"}},
		{"expressions pass through", struct{ N db.Expression }{db.Raw("now()")},
			map[string]any{"n": db.Raw("now()")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := db.Row(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %#v\nwant %#v", got, tc.want)
			}
		})
	}
}

func TestRowErrors(t *testing.T) {
	var nilAccount *account
	tests := map[string]any{
		"map":         map[string]any{"a": 1},
		"int":         5,
		"nil":         nil,
		"nil pointer": nilAccount,
		"no columns":  struct{ _ int }{},
		"all skipped": struct {
			X int `db:"-"`
		}{},
		"slice":        []account{},
		"pointer to 5": new(int),
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := db.Row(in); !errors.Is(err, db.ErrInvalidArgs) {
				t.Errorf("got %v, want ErrInvalidArgs", err)
			}
		})
	}
}

func TestRowsBatchUsesTheUnionOfColumns(t *testing.T) {
	// ID is omitempty: zero in every row -> dropped; non-zero in one row -> kept for all
	rows, err := db.Rows([]account{{Name: "a"}, {Name: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if _, has := r["id"]; has {
			t.Errorf("id should be dropped when zero everywhere: %v", r)
		}
	}
	rows, err = db.Rows([]account{{Name: "a"}, {ID: 9, Name: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["id"] != int64(0) || rows[1]["id"] != int64(9) {
		t.Errorf("id should be written for every row once any row sets it: %v", rows)
	}
	// the result is a valid batch insert: every row has the same columns
	if _, _, err := db.Use(db.Postgres).Table("accounts").InsertSQL(rows...); err != nil {
		t.Errorf("InsertSQL rejected the batch: %v", err)
	}
}

func TestRowsPointersAndEmpty(t *testing.T) {
	rows, err := db.Rows([]*account{{Name: "a"}, {Name: "b"}})
	if err != nil || len(rows) != 2 || rows[1]["name"] != "b" {
		t.Errorf("pointer elements: %v %v", rows, err)
	}
	if rows, err := db.Rows([]account(nil)); rows != nil || err != nil {
		t.Errorf("empty input should be a no-op, got %v %v", rows, err)
	}
	if _, err := db.Rows([]*account{nil}); !errors.Is(err, db.ErrInvalidArgs) {
		t.Errorf("nil element: %v", err)
	}
	if _, err := db.Rows([]int{1}); !errors.Is(err, db.ErrInvalidArgs) {
		t.Errorf("non-struct element: %v", err)
	}
}

func TestRowFeedsTheSQLBuilders(t *testing.T) {
	row, err := db.Row(account{Name: "Ann", Email: str("a@x")})
	if err != nil {
		t.Fatal(err)
	}
	sql, args, err := db.Use(db.Postgres).Table("accounts").InsertSQL(row)
	if err != nil {
		t.Fatal(err)
	}
	if want := `insert into "accounts" ("email", "name", "nick") values ($1, $2, $3)`; sql != want {
		t.Errorf("got %s\nwant %s", sql, want)
	}
	if len(args) != 3 {
		t.Errorf("args = %v", args)
	}
	// an all-omitted struct becomes "default values"
	row, _ = db.Row(struct {
		A int `db:"a,omitempty"`
	}{})
	if sql, _, _ := db.Use(db.Postgres).Table("t").InsertSQL(row); sql != `insert into "t" default values` {
		t.Errorf("got %s", sql)
	}
}
