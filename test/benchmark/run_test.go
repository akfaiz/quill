package benchmark

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/akfaiz/quill/db"
)

// seedRows is the size of the users table every run benchmark reads from.
const seedRows = 1000

// user is the scan target for every library: db tags for quill and sqlx, gorm tags for GORM.
type user struct {
	ID    int64  `db:"id"    gorm:"column:id;primaryKey"`
	Name  string `db:"name"  gorm:"column:name"`
	Age   int    `db:"age"   gorm:"column:age"`
	Email string `db:"email" gorm:"column:email"`
}

func (user) TableName() string { return "users" }

// runFn executes one query end to end (build, send, scan) and returns a value that
// must be identical across libraries: an id, a sum of ids, a count or rows affected.
type runFn func(ctx context.Context) (int64, error)

type runLib struct {
	name string
	fn   runFn
}

type runScenario struct {
	name string
	libs []runLib
}

// runEnv is one in-memory SQLite database shared by every library under test, so the
// engine cost is identical and only the library overhead differs.
type runEnv struct {
	conn *sql.DB
	q    *db.DB
	x    *sqlx.DB
	g    *gorm.DB
}

func newRunEnv(tb testing.TB) *runEnv {
	tb.Helper()

	conn, err := sql.Open("sqlite", ":memory:")
	require.NoError(tb, err)
	// One connection keeps the in-memory database alive and removes pool noise.
	conn.SetMaxOpenConns(1)
	tb.Cleanup(func() { _ = conn.Close() })

	_, err = conn.Exec(`CREATE TABLE users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		age INTEGER NOT NULL,
		email TEXT NOT NULL)`)
	require.NoError(tb, err)

	tx, err := conn.Begin()
	require.NoError(tb, err)
	stmt, err := tx.Prepare(`INSERT INTO users (name, age, email) VALUES (?, ?, ?)`)
	require.NoError(tb, err)
	defer func() { _ = stmt.Close() }()
	for i := 1; i <= seedRows; i++ {
		_, err = stmt.Exec(fmt.Sprintf("user%d", i), 18+i%50, fmt.Sprintf("user%d@example.com", i))
		require.NoError(tb, err)
	}
	require.NoError(tb, tx.Commit())

	g, err := gorm.Open(sqlite.Dialector{Conn: conn}, &gorm.Config{Logger: logger.Discard})
	require.NoError(tb, err)

	return &runEnv{conn: conn, q: db.New(conn, db.SQLite), x: sqlx.NewDb(conn, "sqlite"), g: g}
}

func (e *runEnv) scenarios() []runScenario {
	const (
		oneID  = 500
		minAge = 40
		limit  = 100
		cols   = "id, name, age, email"
	)
	sumIDs := func(us []user) int64 {
		var n int64
		for _, u := range us {
			n += u.ID
		}
		return n
	}
	newUser := func() user { return user{Name: "new", Age: 30, Email: "new@example.com"} }

	return []runScenario{
		{name: "SelectOne", libs: []runLib{
			{"quill", func(ctx context.Context) (int64, error) {
				var u user
				err := e.q.Table("users").Select("id", "name", "age", "email").Find(ctx, oneID, &u)
				return u.ID, err
			}},
			{"sqlx", func(ctx context.Context) (int64, error) {
				var u user
				err := e.x.GetContext(ctx, &u, "SELECT "+cols+" FROM users WHERE id = ? LIMIT 1", oneID)
				return u.ID, err
			}},
			{"gorm", func(ctx context.Context) (int64, error) {
				var u user
				err := e.g.WithContext(ctx).Select("id", "name", "age", "email").Where("id = ?", oneID).Take(&u).Error
				return u.ID, err
			}},
			{"stdlib", func(ctx context.Context) (int64, error) {
				var u user
				err := e.conn.QueryRowContext(ctx, "SELECT "+cols+" FROM users WHERE id = ? LIMIT 1", oneID).
					Scan(&u.ID, &u.Name, &u.Age, &u.Email)
				return u.ID, err
			}},
		}},
		{name: "SelectMany", libs: []runLib{
			{"quill", func(ctx context.Context) (int64, error) {
				var us []user
				err := e.q.Table("users").Select("id", "name", "age", "email").
					Where("age", ">=", minAge).OrderBy("id").Limit(limit).Get(ctx, &us)
				return sumIDs(us), err
			}},
			{"sqlx", func(ctx context.Context) (int64, error) {
				var us []user
				err := e.x.SelectContext(ctx, &us,
					"SELECT "+cols+" FROM users WHERE age >= ? ORDER BY id ASC LIMIT ?", minAge, limit)
				return sumIDs(us), err
			}},
			{"gorm", func(ctx context.Context) (int64, error) {
				var us []user
				err := e.g.WithContext(ctx).Select("id", "name", "age", "email").
					Where("age >= ?", minAge).Order("id").Limit(limit).Find(&us).Error
				return sumIDs(us), err
			}},
			{"stdlib", func(ctx context.Context) (int64, error) {
				rows, err := e.conn.QueryContext(ctx,
					"SELECT "+cols+" FROM users WHERE age >= ? ORDER BY id ASC LIMIT ?", minAge, limit)
				if err != nil {
					return 0, err
				}
				defer rows.Close()
				var n int64
				for rows.Next() {
					var u user
					if err := rows.Scan(&u.ID, &u.Name, &u.Age, &u.Email); err != nil {
						return 0, err
					}
					n += u.ID
				}
				return n, rows.Err()
			}},
		}},
		{name: "Count", libs: []runLib{
			{"quill", func(ctx context.Context) (int64, error) {
				return e.q.Table("users").Where("age", ">=", minAge).Count(ctx)
			}},
			{"sqlx", func(ctx context.Context) (int64, error) {
				var n int64
				err := e.x.GetContext(ctx, &n, "SELECT COUNT(*) FROM users WHERE age >= ?", minAge)
				return n, err
			}},
			{"gorm", func(ctx context.Context) (int64, error) {
				var n int64
				err := e.g.WithContext(ctx).Model(&user{}).Where("age >= ?", minAge).Count(&n).Error
				return n, err
			}},
			{"stdlib", func(ctx context.Context) (int64, error) {
				var n int64
				err := e.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE age >= ?", minAge).Scan(&n)
				return n, err
			}},
		}},
		{name: "Insert", libs: []runLib{
			{"quill", func(ctx context.Context) (int64, error) {
				u := newUser()
				return e.q.Table("users").Insert(ctx, map[string]any{"name": u.Name, "age": u.Age, "email": u.Email})
			}},
			{"sqlx", func(ctx context.Context) (int64, error) {
				u := newUser()
				res, err := e.x.ExecContext(ctx, "INSERT INTO users (name, age, email) VALUES (?, ?, ?)",
					u.Name, u.Age, u.Email)
				if err != nil {
					return 0, err
				}
				return res.RowsAffected()
			}},
			{"gorm", func(ctx context.Context) (int64, error) {
				u := newUser()
				res := e.g.WithContext(ctx).Select("name", "age", "email").Create(&u)
				return res.RowsAffected, res.Error
			}},
			{"stdlib", func(ctx context.Context) (int64, error) {
				u := newUser()
				res, err := e.conn.ExecContext(ctx, "INSERT INTO users (name, age, email) VALUES (?, ?, ?)",
					u.Name, u.Age, u.Email)
				if err != nil {
					return 0, err
				}
				return res.RowsAffected()
			}},
		}},
		{name: "Update", libs: []runLib{
			{"quill", func(ctx context.Context) (int64, error) {
				return e.q.Table("users").Where("id", oneID).Update(ctx, map[string]any{"age": 31})
			}},
			{"sqlx", func(ctx context.Context) (int64, error) {
				res, err := e.x.ExecContext(ctx, "UPDATE users SET age = ? WHERE id = ?", 31, oneID)
				if err != nil {
					return 0, err
				}
				return res.RowsAffected()
			}},
			{"gorm", func(ctx context.Context) (int64, error) {
				res := e.g.WithContext(ctx).Model(&user{}).Where("id = ?", oneID).Update("age", 31)
				return res.RowsAffected, res.Error
			}},
			{"stdlib", func(ctx context.Context) (int64, error) {
				res, err := e.conn.ExecContext(ctx, "UPDATE users SET age = ? WHERE id = ?", 31, oneID)
				if err != nil {
					return 0, err
				}
				return res.RowsAffected()
			}},
		}},
	}
}

// TestRunScenariosAreComparable guards the comparison: every library must return the
// same result for the same query, so a benchmark cannot win by doing less work.
func TestRunScenariosAreComparable(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"SelectOne", "SelectMany", "Count", "Insert", "Update"} {
		t.Run(name, func(t *testing.T) {
			e := newRunEnv(t)
			for _, s := range e.scenarios() {
				if s.name != name {
					continue
				}
				want, err := s.libs[0].fn(ctx)
				require.NoError(t, err, s.libs[0].name)
				assert.NotZero(t, want, s.libs[0].name)
				for _, l := range s.libs[1:] {
					got, err := l.fn(ctx)
					require.NoError(t, err, l.name)
					assert.Equal(t, want, got, "%s returns a different result", l.name)
				}
			}
		})
	}
}

// BenchmarkRun measures build + execute + scan against an in-memory SQLite database.
// Each scenario gets a fresh, identically seeded database.
func BenchmarkRun(b *testing.B) {
	ctx := context.Background()
	names := []string{"SelectOne", "SelectMany", "Count", "Insert", "Update"}
	for _, name := range names {
		b.Run(name, func(b *testing.B) {
			for i := range 4 {
				e := newRunEnv(b)
				var s runScenario
				for _, c := range e.scenarios() {
					if c.name == name {
						s = c
					}
				}
				l := s.libs[i]
				b.Run(l.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if _, err := l.fn(ctx); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
