package integration

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/akfaiz/quill/db"
)

// Helpers shared by the SQLite and the PostgreSQL/MySQL suites.

var ctx = context.Background()

type User struct {
	ID     int64
	Name   string
	Email  *string
	Age    *int
	Active bool
	Score  float64
	Bio    string // NULL in some rows: scans to ""
}

func names(us []User) []string {
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = u.Name
	}
	return out
}

func eq(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func pluckNames(t *testing.T, b *db.Builder) []string {
	t.Helper()
	var out []string
	noErr(t, b.Pluck(ctx, "name", &out))
	return out
}

// Member maps the users table for struct writes: omitempty keeps unset fields
// out of INSERT/UPDATE so database defaults and existing values are untouched.
type Member struct {
	ID    int64   `db:"id,omitempty"`
	Name  string  `db:"name,omitempty"`
	Email *string `db:"email,omitempty"`
	Age   *int    `db:"age,omitempty"`
	Bio   string  `db:"bio,omitempty"`
}

func ptr[T any](v T) *T { return &v }

func (User) TableName() string   { return "users" }
func (Member) TableName() string { return "users" }

type Post struct {
	ID    int64  `db:"id,omitempty"`
	Title string `db:"title"`
	Views int    `db:"views,omitempty"`
}

func (*Post) TableName() string { return "posts" } // pointer receiver works too

// Untitled has no TableName method: From[Untitled] must fail clearly.
type Untitled struct{ ID int64 }

// seedStatements insert the same four users and four posts on PostgreSQL and MySQL.
var seedStatements = []string{
	`insert into users (name, email, age, active, score, bio, meta, created_at) values
	  ('Ann', 'ann@x', 30, true,  1.5, null,    '{"tags":["a","b"],"theme":"dark","n":3}', '2024-03-07 09:05:02'),
	  ('Bob', 'bob@x', 25, false, 2.5, null,    '{"tags":["b"],"theme":"light"}',          '2024-03-08 10:00:00'),
	  ('Cy',  'cy@x',  41, true,  3.5, 'hello', '{}',                                      '2023-12-31 23:59:59'),
	  ('Di',  'di@x',  null, true, 0, null,     null,                                      null)`,
	`insert into posts (user_id, title, views, published) values
	  (1, 'p1', 10, true), (1, 'p2', 5, false), (2, 'p3', 7, true), (3, 'p4', 1, true)`,
}

// execAll runs statements in order, failing the test on the first error.
func execAll(t *testing.T, raw *sql.DB, stmts []string) {
	t.Helper()
	for _, st := range stmts {
		if _, err := raw.ExecContext(ctx, st); err != nil {
			t.Fatalf("setup failed: %v\n%s", err, st)
		}
	}
}
