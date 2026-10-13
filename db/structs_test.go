package db_test

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"

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
		{
			"non-zero omitempty fields are kept",
			account{ID: 7, Name: "Ann", Email: str("a@x"), Bio: "hi", Created: at},
			map[string]any{
				"id":         int64(7),
				"name":       "Ann",
				"email":      str("a@x"),
				"bio":        "hi",
				"created_at": at,
				"nick":       sql.NullString{},
			},
		},
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
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
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

func TestInsertRowsBatchUsesTheUnionOfColumns(t *testing.T) {
	ins := func(rows ...account) *db.InsertBuilder {
		return db.Use(db.Postgres).Insert("accounts").Rows(rows)
	}
	// ID is omitempty: zero in every row -> dropped; non-zero in one row -> kept for all
	q, _, err := ins(account{Name: "a"}, account{Name: "b"}).ToSQL()
	require.NoError(t, err)
	assert.NotContains(t, q, `"id"`)

	q, args, err := ins(account{Name: "a"}, account{ID: 9, Name: "b"}).ToSQL()
	require.NoError(t, err)
	assert.Contains(t, q, `"id"`)
	assert.Contains(t, args, int64(9))
	assert.Contains(t, args, int64(0)) // the first row gets the column too
}

func TestInsertRowsPointersAndEmpty(t *testing.T) {
	ins := func(v ...any) *db.InsertBuilder { return db.Use(db.Postgres).Insert("accounts").Rows(v...) }

	_, args, err := ins([]*account{{Name: "a"}, {Name: "b"}}).ToSQL()
	require.NoError(t, err)
	assert.Contains(t, args, "b")

	_, _, err = ins([]account(nil)).ToSQL()
	require.ErrorIs(t, err, db.ErrNoValues)

	_, _, err = ins([]*account{nil}).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)

	_, _, err = ins([]int{1}).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)
}

func TestRowFeedsTheSQLBuilders(t *testing.T) {
	row, err := db.Row(account{Name: "Ann", Email: str("a@x")})
	require.NoError(t, err)
	sql, args, err := db.Use(db.Postgres).Insert("accounts").Values(row).ToSQL()
	require.NoError(t, err)
	if want := `INSERT INTO "accounts" ("email", "name", "nick") VALUES ($1, $2, $3)`; sql != want {
		t.Errorf("got %s\nwant %s", sql, want)
	}
	require.Len(t, args, 3)
	// an all-omitted struct becomes "default values"
	row, _ = db.Row(struct {
		A int `db:"a,omitempty"`
	}{})
	if sql, _, _ := db.Use(db.Postgres).Insert("t").Values(row).ToSQL(); sql != `INSERT INTO "t" DEFAULT VALUES` {
		t.Errorf("got %s", sql)
	}
}

func TestStructWritesViaBuilders(t *testing.T) {
	q, args, err := db.Use(db.Postgres).Insert("accounts").Rows(account{Name: "Ann", Email: str("a@x")}).ToSQL()
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "accounts" ("email", "name", "nick") VALUES ($1, $2, $3)`, q)
	require.Len(t, args, 3)

	q, args, err = db.Use(db.Postgres).
		Update("accounts").
		SetRow(account{Name: "Ann", Email: str("a@x")}).
		Where("id", 1).
		ToSQL()
	require.NoError(t, err)
	assert.Contains(t, q, `UPDATE "accounts" SET `)
	assert.Len(t, args, 4)

	_, _, err = db.Use(db.Postgres).Update("accounts").SetRow(42).Where("id", 1).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)
}
