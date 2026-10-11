package integration

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/akfaiz/quill/db"
)

type Base struct {
	ID int64
}

type Embedded struct {
	Base
	Name      string
	UserEmail string `db:"email"`
	Ignored   string `db:"-"`
}

const schema = `
create table users (
  id integer primary key autoincrement,
  name text not null,
  email text unique,
  age integer,
  active integer not null default 1,
  score real not null default 0,
  bio text,
  meta text,
  created_at text
);
create table posts (
  id integer primary key autoincrement,
  user_id integer,
  title text,
  views integer not null default 0,
  published integer not null default 0
);
insert into users (name, email, age, active, score, bio, meta, created_at) values
 ('Ann', 'ann@x', 30, 1, 1.5, null,    '{"tags":["a","b"],"theme":"dark","n":3}', '2024-03-07 09:05:02'),
 ('Bob', 'bob@x', 25, 0, 2.5, null,    '{"tags":["b"],"theme":"light"}',          '2024-03-08 10:00:00'),
 ('Cy',  'cy@x',  41, 1, 3.5, 'hello', '{}',                                      '2023-12-31 23:59:59'),
 ('Di',  'di@x',  null, 1, 0, null,    null,                                      null);
insert into posts (user_id, title, views, published) values
 (1, 'p1', 10, 1), (1, 'p2', 5, 0), (2, 'p3', 7, 1), (3, 'p4', 1, 1);
`

func newDB(t *testing.T, opts ...db.Option) *db.DB {
	t.Helper()
	sdb, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	sdb.SetMaxOpenConns(1) // each :memory: connection is its own database
	t.Cleanup(func() { _ = sdb.Close() })
	if _, err := sdb.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db.New(sdb, db.SQLite, opts...)
}

func TestSQLiteScanning(t *testing.T) {
	conn := newDB(t)

	t.Run("structs with NULL guard and pointer fields", func(t *testing.T) {
		var us []User
		require.NoError(t, conn.Select().From("users").OrderBy("id").Get(ctx, &us))
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, names(us))
		assert.Equal(t, "hello", us[2].Bio)
		assert.Empty(t, us[0].Bio) // NULL -> zero value
		if us[3].Age != nil || us[0].Age == nil || *us[0].Age != 30 {
			t.Errorf("pointer NULL handling wrong: %+v %+v", us[3].Age, us[0].Age)
		}
		assert.False(t, us[1].Active)
		assert.Equal(t, 1.5, us[0].Score)
	})
	t.Run("pointer elements", func(t *testing.T) {
		var us []*User
		require.NoError(t, conn.Select().From("users").OrderBy("id").Limit(2).Get(ctx, &us))
		assert.Len(t, us, 2)
		assert.Equal(t, "Bob", us[1].Name)
	})
	t.Run("maps convert bytes and keep NULL as nil", func(t *testing.T) {
		var rows []map[string]any
		require.NoError(t, conn.Select("name", "age").From("users").OrderBy("id").Get(ctx, &rows))
		assert.Equal(t, "Ann", rows[0]["name"])
		assert.Equal(t, int64(30), rows[0]["age"])
		assert.Nil(t, rows[3]["age"])
	})
	t.Run("embedded structs, tag, conn:- and case-insensitive fallback", func(t *testing.T) {
		var es []Embedded
		require.NoError(t, conn.Select("id", "NAME", "email").From("users").OrderBy("id").Limit(1).Get(ctx, &es))
		assert.Equal(t, Embedded{Base: Base{1}, Name: "Ann", UserEmail: "ann@x"}, es[0])
	})
	t.Run("unknown columns are ignored", func(t *testing.T) {
		var us []User
		require.NoError(t, conn.Select("id", "name", "meta").From("users").Limit(1).OrderBy("id").Get(ctx, &us))
		assert.Equal(t, "Ann", us[0].Name)
	})
	t.Run("empty result is an empty non-nil slice", func(t *testing.T) {
		var us []User
		require.NoError(t, conn.Select().From("users").Where("id", 999).Get(ctx, &us))
		if us == nil || len(us) != 0 {
			t.Errorf("want empty non-nil slice, got %#v", us)
		}
	})
	t.Run("bad destinations", func(t *testing.T) {
		var u User
		require.ErrorIs(t, conn.Select().From("users").Get(ctx, &u), db.ErrInvalidArgs)
		require.ErrorIs(t, conn.Select().From("users").Get(ctx, nil), db.ErrInvalidArgs)
		var names []string
		require.Error(t, conn.Select("id", "name").From("users").Get(ctx, &names))
	})
	t.Run("generic helpers", func(t *testing.T) {
		us, err := db.As[User](conn.Select().From("users").OrderBy("id")).Get(ctx)
		require.NoError(t, err)
		assert.Len(t, us, 4)
		u, err := db.As[User](conn.Select().From("users").Where("name", "Cy")).First(ctx)
		require.NoError(t, err)
		assert.True(t, u.Age != nil && *u.Age == 41)
		_, err = db.As[User](conn.Select().From("users").Where("name", "nobody")).First(ctx)
		assert.ErrorIs(t, err, db.ErrNotFound)
	})
}

func TestSQLiteReads(t *testing.T) {
	conn := newDB(t)
	users := func() *db.SelectBuilder { return conn.Select().From("users") }

	t.Run("first find sole", func(t *testing.T) {
		var u User
		require.NoError(t, users().OrderBy("age", "desc").First(ctx, &u))
		assert.Equal(t, "Cy", u.Name)
		require.NoError(t, users().Find(ctx, 2, &u))
		assert.Equal(t, "Bob", u.Name)
		require.ErrorIs(t, users().Find(ctx, 99, &u), sql.ErrNoRows)
		require.NoError(t, users().Where("name", "Ann").Sole(ctx, &u))
		assert.Equal(t, db.ErrMultipleRecords, users().Where("age", ">", 0).Sole(ctx, &u))
		assert.Equal(t, db.ErrNotFound, users().Where("name", "zzz").Sole(ctx, &u))
	})
	t.Run("value pluck pluckmap", func(t *testing.T) {
		var age int
		require.NoError(t, users().Where("name", "Bob").Value(ctx, "age", &age))
		assert.Equal(t, 25, age)
		var ns []string
		require.NoError(t, users().Where("active", 1).OrderBy("name").Pluck(ctx, "name", &ns))
		assert.Equal(t, []string{"Ann", "Cy", "Di"}, ns)
		var m map[int64]string
		require.NoError(t, users().PluckMap(ctx, "name", "id", &m))
		assert.Equal(t, map[int64]string{1: "Ann", 2: "Bob", 3: "Cy", 4: "Di"}, m)
		var nullable []int // NULL age -> 0
		require.NoError(t, users().OrderBy("id").Pluck(ctx, "age", &nullable))
		assert.Equal(t, []int{30, 25, 41, 0}, nullable)
		assert.ErrorIs(t, users().PluckMap(ctx, "name", "id", &ns), db.ErrInvalidArgs)
	})
	t.Run("exists", func(t *testing.T) {
		ok, err := users().Where("name", "Ann").Exists(ctx)
		require.NoError(t, err)
		assert.True(t, ok)
		no, err := users().Where("name", "zzz").DoesntExist(ctx)
		require.NoError(t, err)
		assert.True(t, no)
	})
	t.Run("aggregates", func(t *testing.T) {
		n, err := users().Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(4), n)
		n, err = users().Count(ctx, "age") // count(col) skips NULL
		require.NoError(t, err)
		assert.Equal(t, int64(3), n)
		sum, err := users().Sum(ctx, "age")
		require.NoError(t, err)
		assert.Equal(t, 96.0, sum)
		avg, err := users().Avg(ctx, "age")
		require.NoError(t, err)
		assert.Equal(t, 32.0, avg)
		var mn, mx int
		require.NoError(t, users().Min(ctx, "age", &mn))
		require.NoError(t, users().Max(ctx, "age", &mx))
		assert.Equal(t, [2]int{25, 41}, [2]int{mn, mx})
		empty, err := users().Where("id", 99).Sum(ctx, "age") // NULL -> 0
		require.NoError(t, err)
		assert.Equal(t, 0.0, empty)
		// ordering must not break an aggregate
		n, err = users().OrderBy("name").Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(4), n)
	})
	t.Run("aggregates over grouped, distinct and limited queries are wrapped", func(t *testing.T) {
		n, err := conn.Select().From("posts").GroupBy("user_id").Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(3), n) // 3 groups, not 4 rows
		n, err = conn.Select("user_id").From("posts").Distinct().Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(3), n)
		n, err = conn.Select().From("users").OrderBy("id").Limit(2).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Select().From("posts").Join("users", "users.id", "posts.user_id").Where("posts.views", ">", 0).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(4), n)
		total, err := conn.Select().From("posts").
			Limit(3).
			OrderBy("id").
			Sum(ctx, "posts.views")
			// qualified col in wrapped query
		require.NoError(t, err)
		assert.Equal(t, 22.0, total)
	})
	t.Run("not bound and cancelled context", func(t *testing.T) {
		var us []User
		assert.Equal(t, db.ErrNotBound, db.Use(db.SQLite).Select().From("users").Get(ctx, &us))
		_, err := db.Use(db.SQLite).Select().From("users").Count(ctx)
		assert.Equal(t, db.ErrNotBound, err)
		c, cancel := context.WithCancel(ctx)
		cancel()
		require.Error(t, users().Get(c, &us))
	})
	t.Run("builder errors surface from executors", func(t *testing.T) {
		var us []User
		assert.ErrorIs(t, users().Where("a", "bogus", 1).Get(ctx, &us), db.ErrInvalidOperator)
	})
}

func TestSQLiteQueryShapesExecute(t *testing.T) {
	conn := newDB(t)
	pluck := func(t *testing.T, b *db.SelectBuilder) []string {
		t.Helper()
		var out []string
		require.NoError(t, b.Pluck(ctx, "name", &out))
		return out
	}
	users := func() *db.SelectBuilder { return conn.Select().From("users").OrderBy("users.id") }

	cases := []struct {
		name string
		q    *db.SelectBuilder
		want []string
	}{
		{"in / not in", users().WhereIn("id", []int{1, 3}).WhereNotIn("name", []string{"Cy"}), []string{"Ann"}},
		{"empty in", users().WhereIn("id", []int{}), nil},
		{"between", users().WhereBetween("age", 26, 45), []string{"Ann", "Cy"}},
		{"null / not null", users().WhereNull("age").OrWhereNull("bio"), []string{"Ann", "Bob", "Di"}},
		{"like insensitive", users().WhereLike("name", "a%"), []string{"Ann"}},
		{"like case sensitive uses glob", users().WhereLike("name", "a%", true), nil},
		{"like case sensitive hit", users().WhereLike("name", "A%", true), []string{"Ann"}},
		{"not like", users().WhereNotLike("name", "%n"), []string{"Bob", "Cy", "Di"}},
		{"null safe equals on NULL", users().WhereNullSafeEquals("age", nil), []string{"Di"}},
		{
			"group",
			users().Where("active", 1).WhereGroup(func(q *db.WhereClause) { q.Where("age", ">", 35).OrWhereNull("age") }),
			[]string{"Cy", "Di"},
		},
		{"not", users().WhereNot("active", 1), []string{"Bob"}},
		{
			"exists subquery",
			users().WhereExists(conn.Select().From("posts").SelectRaw("1").WhereColumn("posts.user_id", "users.id").Where("views", ">", 6)),
			[]string{"Ann", "Bob"},
		},
		{
			"in subquery",
			users().WhereIn("id", conn.Select("user_id").From("posts").Where("published", 1)),
			[]string{"Ann", "Bob", "Cy"},
		},
		{
			"scalar subquery compare",
			users().Where("age", ">", conn.Select().From("users").SelectRaw("avg(age)")),
			[]string{"Cy"},
		},
		{"any / all / none", users().WhereAny([]string{"name", "email"}, "like", "b%"), []string{"Bob"}},
		{
			"date parts",
			users().WhereYear("created_at", 2024).WhereMonth("created_at", 3).WhereDay("created_at", ">", 7),
			[]string{"Bob"},
		},
		{
			"date and time",
			users().WhereDate("created_at", "2024-03-07").WhereTime("created_at", ">=", "09:00:00"),
			[]string{"Ann"},
		},
		{"row values", users().WhereRowValues([]string{"id", "age"}, "=", []any{1, 30}), []string{"Ann"}},
		{"raw", users().WhereRaw("length(name) = ?", 2), []string{"Cy", "Di"}},
		{"order latest limit offset", conn.Select().From("users").Latest("id").Limit(2).Offset(1), []string{"Cy", "Bob"}},
		{
			"left join with value",
			users().LeftJoinFunc("posts", func(j *db.JoinClause) { j.On("posts.user_id", "users.id").Where("posts.views", ">", 8) }).
				WhereNull("posts.id"),
			[]string{"Bob", "Cy", "Di"},
		},
		{
			"join sub",
			users().JoinSub(conn.Select("user_id").From("posts").SelectRaw("max(views) as mv").GroupBy("user_id"), "m", "m.user_id", "users.id").
				Where("m.mv", ">", 6),
			[]string{"Ann", "Bob"},
		},
		{
			"cross join",
			conn.Select().From("users").CrossJoin("posts").Where("users.id", 1).Where("posts.id", 1),
			[]string{"Ann"},
		},
		{
			"from sub",
			conn.Select().FromSub(conn.Select().From("users").Where("active", 1), "au").OrderBy("name"),
			[]string{"Ann", "Cy", "Di"},
		},
		{
			"union",
			conn.Select().From("users").
				Select("name").
				Where("id", 1).
				Union(conn.Select("name").From("users").Where("id", 2)).
				OrderBy("name", "desc"),
			[]string{"Bob", "Ann"},
		},
		{
			"union all with limit",
			conn.Select().From("users").
				Select("name").
				Where("id", 1).
				UnionAll(conn.Select("name").From("users").Where("id", 1)).
				Limit(5),
			[]string{"Ann", "Ann"},
		},
		{"shared lock is a no-op on sqlite", users().SharedLock().Limit(1), []string{"Ann"}},
		{
			"force index",
			conn.Select().From("users").ForceIndex("sqlite_autoindex_users_1").Where("email", "cy@x"),
			[]string{"Cy"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pluck(t, tc.q)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}

	// these only need to be valid SQL for the engine
	for name, q := range map[string]*db.SelectBuilder{
		"column compare":  conn.Select().From("posts").WhereColumn("views", ">", "user_id").OrderBy("id"),
		"between columns": conn.Select().From("users").WhereBetweenColumns("age", "id", "score"),
		"random order":    conn.Select().From("users").InRandomOrder().Limit(1),
		"group having":    conn.Select("user_id").From("posts").SelectRaw("sum(views) as v").GroupBy("user_id").Having("v", ">", 6),
	} {
		t.Run("executes: "+name, func(t *testing.T) {
			var rows []map[string]any
			require.NoError(t, q.Get(ctx, &rows))
		})
	}

	t.Run("select sub value", func(t *testing.T) {
		var rows []map[string]any
		require.NoError(
			t,
			conn.Select().From("users").
				Select("name").
				SelectSub(conn.Select().From("posts").SelectRaw("count(*)").WhereColumn("posts.user_id", "users.id"), "pc").
				OrderBy("id").
				Get(ctx, &rows),
		)
		assert.Equal(t, int64(2), rows[0]["pc"])
		assert.Equal(t, int64(0), rows[3]["pc"])
	})
	t.Run("lateral join is rejected on sqlite", func(t *testing.T) {
		var rows []map[string]any
		err := conn.Select().From("users").JoinLateral(conn.Select().From("posts"), "p").Get(ctx, &rows)
		assert.ErrorIs(t, err, db.ErrUnsupported)
	})
}

func TestSQLiteJSONExecution(t *testing.T) {
	conn := newDB(t)
	pluck := func(b *db.SelectBuilder) []string {
		var out []string
		require.NoError(t, b.OrderBy("id").Pluck(ctx, "name", &out))
		return out
	}
	t.Run("contains / doesn't contain", func(t *testing.T) {
		assert.Equal(t, []string{"Ann", "Bob"}, pluck(conn.Select().From("users").WhereJsonContains("meta->tags", "b")))
		assert.Equal(t, []string{"Ann"}, pluck(conn.Select().From("users").WhereJsonContains("meta->tags", "a")))
		assert.Equal(
			t,
			[]string{"Bob", "Cy"},
			pluck(conn.Select().From("users").WhereJsonDoesntContain("meta->tags", "a").WhereNotNull("meta")),
		)
	})
	t.Run("contains key / length / selector", func(t *testing.T) {
		assert.Equal(t, []string{"Ann", "Bob"}, pluck(conn.Select().From("users").WhereJsonContainsKey("meta->theme")))
		assert.Equal(t, []string{"Ann"}, pluck(conn.Select().From("users").WhereJsonLength("meta->tags", ">", 1)))
		assert.Equal(t, []string{"Bob"}, pluck(conn.Select().From("users").Where("meta->theme", "light")))
		var themes []string
		require.NoError(t, conn.Select().From("users").Where("id", 1).Pluck(ctx, "meta->theme", &themes))
		assert.Equal(t, []string{"dark"}, themes)
	})
	t.Run("json updates patch in place", func(t *testing.T) {
		n, err := conn.Update("users").
			Where("id", 1).SetMap(map[string]any{"meta->theme": "blue", "meta->flags->beta": true}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		var theme string
		var beta bool
		require.NoError(t, conn.Select().From("users").Where("id", 1).Value(ctx, "meta->theme", &theme))
		assert.Equal(t, "blue", theme)
		require.NoError(t, conn.Select().From("users").Where("id", 1).Value(ctx, "meta->flags->beta", &beta))
		assert.True(t, beta)
		// untouched siblings survive the patch
		assert.Equal(t, []string{"Ann"}, pluck(conn.Select().From("users").WhereJsonContains("meta->tags", "a")))
	})
	t.Run("boolean comparison", func(t *testing.T) {
		assert.Equal(t, []string{"Ann"}, pluck(conn.Select().From("users").Where("meta->flags->beta", true)))
	})
}

// TestSQLiteJSONUpdateSemantics pins that a JSON path update sets values the way
// PostgreSQL's jsonb_set and MySQL's JSON_SET do (the old JSON_PATCH form deleted a
// key set to nil, merged map values and wrote "tags[0]" as a literal key).
func TestSQLiteJSONUpdateSemantics(t *testing.T) {
	metaOf := func(t *testing.T, conn *db.DB, id int) string {
		t.Helper()
		var meta string
		require.NoError(t, conn.Select().From("users").Where("id", id).Value(ctx, "meta", &meta))
		return meta
	}
	update := func(t *testing.T, conn *db.DB, id int, set map[string]any) {
		t.Helper()
		n, err := conn.Update("users").Where("id", id).SetMap(set).Exec(ctx)
		require.NoError(t, err)
		require.Equal(t, int64(1), n)
	}
	t.Run("nil is JSON null, not a deleted key", func(t *testing.T) {
		conn := newDB(t)
		update(t, conn, 1, map[string]any{"meta->theme": nil})
		assert.JSONEq(t, `{"tags":["a","b"],"theme":null,"n":3}`, metaOf(t, conn, 1))
	})
	t.Run("a map replaces an old object instead of merging into it", func(t *testing.T) {
		conn := newDB(t)
		update(t, conn, 1, map[string]any{"meta->flags": map[string]any{"y": 2}})
		update(t, conn, 1, map[string]any{"meta->flags": map[string]any{"x": 1}})
		assert.JSONEq(t, `{"tags":["a","b"],"theme":"dark","n":3,"flags":{"x":1}}`, metaOf(t, conn, 1))
	})
	t.Run("array index sets one element", func(t *testing.T) {
		conn := newDB(t)
		update(t, conn, 1, map[string]any{"meta->tags[0]": "z"})
		assert.JSONEq(t, `{"tags":["z","b"],"theme":"dark","n":3}`, metaOf(t, conn, 1))
	})
	t.Run("several paths of one column in one statement", func(t *testing.T) {
		conn := newDB(t)
		update(t, conn, 1, map[string]any{"meta->a->b": 1, "meta->a->c": []string{"q"}, "meta->n": 4.5})
		assert.JSONEq(t, `{"tags":["a","b"],"theme":"dark","n":4.5,"a":{"b":1,"c":["q"]}}`, metaOf(t, conn, 1))
	})
	t.Run("a NULL column becomes an object", func(t *testing.T) {
		conn := newDB(t)
		update(t, conn, 4, map[string]any{"meta->theme": "dark"})
		assert.JSONEq(t, `{"theme":"dark"}`, metaOf(t, conn, 4))
	})
	t.Run("string, bool and number keep their JSON types", func(t *testing.T) {
		conn := newDB(t)
		update(t, conn, 3, map[string]any{"meta->s": "42", "meta->b": false, "meta->i": 7})
		assert.JSONEq(t, `{"s":"42","b":false,"i":7}`, metaOf(t, conn, 3))
	})
}

func TestSQLiteWrites(t *testing.T) {
	t.Run("insert variants", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Insert("users").Values(map[string]any{"name": "Eve", "email": "eve@x"}, map[string]any{"name": "Fay", "email": "fay@x"}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Insert("users").Exec(ctx) // no rows: no-op
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		id, err := conn.Insert("users").Values(map[string]any{"name": "Gus", "email": "gus@x"}).ExecGetID(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(7), id)
		n, err = conn.Insert("users").Values(map[string]any{"name": "Dup", "email": "ann@x"}).Ignore().Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n) // conflicts on unique email
		n, err = conn.Insert("posts").Using([]string{"user_id", "title"}, conn.Select("id").From("users").SelectRaw("'from ' || name").Where("id", "<=", 2)).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		var t1 string
		require.NoError(t, conn.Select().From("posts").Where("title", "like", "from%").OrderBy("id").Value(ctx, "title", &t1))
		assert.Equal(t, "from Ann", t1)
		_, err = conn.Insert("users").Values(map[string]any{"name": "Ann2", "email": "ann@x"}).Exec(ctx)
		require.Error(t, err)
	})
	t.Run("upsert and update-or-insert", func(t *testing.T) {
		conn := newDB(t)
		_, err := conn.Insert("users").Values([]map[string]any{
			{"name": "Ann!", "email": "ann@x", "age": 31},
			{"name": "New", "email": "new@x", "age": 1},
		}...).Upsert([]string{"email"}, []string{"name", "age"}).Exec(ctx)
		require.NoError(t, err)
		var name string
		require.NoError(t, conn.Select().From("users").Where("email", "ann@x").Value(ctx, "name", &name))
		assert.Equal(t, "Ann!", name)
		c, _ := conn.Select().From("users").Count(ctx)
		assert.Equal(t, int64(5), c)

		ok, err := conn.UpdateOrInsert(ctx, "users", map[string]any{"email": "zed@x"}, map[string]any{"name": "Zed"})
		require.NoError(t, err)
		assert.True(t, ok)
		ok, err = conn.UpdateOrInsert(ctx, "users", map[string]any{"email": "zed@x"}, map[string]any{"name": "Zed2"})
		require.NoError(t, err)
		assert.True(t, ok)
		require.NoError(t, conn.Select().From("users").Where("email", "zed@x").Value(ctx, "name", &name))
		assert.Equal(t, "Zed2", name)
		c, _ = conn.Select().From("users").Count(ctx)
		assert.Equal(t, int64(6), c)
	})
	t.Run("update increment decrement", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Update("users").Where("active", 1).SetMap(map[string]any{"bio": "on"}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(3), n)
		n, err = conn.Update("posts").Where("user_id", 1).Increment("views", 5).SetMap(map[string]any{"title": "bumped"}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Update("posts").Where("id", 1).DecrementEach(map[string]any{"views": 1, "published": 1}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		var rows []map[string]any
		require.NoError(t, conn.Select().From("posts").Where("id", 1).Get(ctx, &rows))
		assert.Equal(t, int64(14), rows[0]["views"])
		assert.Equal(t, int64(0), rows[0]["published"])
		assert.Equal(t, "bumped", rows[0]["title"])
		_, err = conn.Update("users").SetMap(map[string]any{"bio": "x"}).Exec(ctx)
		assert.Equal(t, db.ErrNoWhere, err)
		n, err = conn.Update("users").AllRows().SetMap(map[string]any{"bio": "all"}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(4), n)
	})
	t.Run("decrement, increment each, insert-or-ignore-using, update-from", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Update("posts").Where("id", 1).Decrement("views", 3).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		n, err = conn.Update("posts").
			Where("user_id", 1).IncrementEach(map[string]any{"views": 1, "published": 1}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		var v int
		require.NoError(t, conn.Select().From("posts").Where("id", 1).Value(ctx, "views", &v))
		assert.Equal(t, 8, v) // 10 - 3 + 1

		n, err = conn.Insert("users").Using([]string{"name", "email"}, conn.Select("name").From("users").SelectRaw("email || 'x'").Where("id", "<=", 2)).Ignore().Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Insert("users").Using([]string{"name", "email"}, // same emails again: ignored
			conn.Select("name").From("users").SelectRaw("email || 'x'").Where("id", "<=", 2)).Ignore().Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)

		_, err = conn.Update("users").Where("id", 1).FromJoins().SetMap(map[string]any{"name": "x"}).Exec(ctx)
		assert.ErrorIs(t, err, db.ErrUnsupported)
	})
	t.Run("update with join and limit go through rowid", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Update("users as u").
			Join("posts as p", "p.user_id", "u.id").
			Where("p.views", ">", 6).SetMap(map[string]any{"u.active": 0}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Update("posts").
			Where("views", ">", 0).
			OrderBy("views", "desc").
			Limit(1).SetMap(map[string]any{"title": "top"}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		var title string
		require.NoError(t, conn.Select().From("posts").Where("views", 10).Value(ctx, "title", &title))
		assert.Equal(t, "top", title)
	})
	t.Run("delete variants", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Delete("posts").Where("published", 0).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		n, err = conn.Delete("posts").Where("views", ">", 0).OrderBy("views").Limit(1).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		n, err = conn.Delete("posts as p").Join("users as u", "u.id", "p.user_id").Where("u.name", "Bob").Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		_, err = conn.Delete("posts").Exec(ctx)
		assert.Equal(t, db.ErrNoWhere, err)
		n, err = conn.Delete("posts").AllRows().Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
	})
	t.Run("truncate resets autoincrement", func(t *testing.T) {
		conn := newDB(t)
		require.NoError(t, conn.Truncate(ctx, "posts"))
		c, _ := conn.Select().From("posts").Count(ctx)
		assert.Equal(t, int64(0), c)
		id, err := conn.Insert("posts").Values(map[string]any{"title": "fresh"}).ExecGetID(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), id)
	})
	t.Run("truncate without any AUTOINCREMENT table", func(t *testing.T) {
		sdb, err := sql.Open("sqlite", ":memory:")
		require.NoError(t, err)
		sdb.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = sdb.Close() })
		// INTEGER PRIMARY KEY without AUTOINCREMENT: SQLite never creates sqlite_sequence
		_, err = sdb.Exec(`CREATE TABLE notes (id INTEGER PRIMARY KEY, body TEXT)`)
		require.NoError(t, err)
		conn := db.New(sdb, db.SQLite)
		_, err = conn.Insert("notes").Values(map[string]any{"body": "a"}).Exec(ctx)
		require.NoError(t, err)

		require.NoError(t, conn.Truncate(ctx, "notes"))
		c, err := conn.Select().From("notes").Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), c)
	})
	t.Run("returning", func(t *testing.T) {
		conn := newDB(t)
		var out []struct {
			ID   int64
			Name string
		}
		require.NoError(
			t,
			conn.Insert("users").
				Returning("id", "name").Values(map[string]any{"name": "Rae", "email": "rae@x"}).Get(ctx, &out),
		)
		assert.Len(t, out, 1)
		assert.Equal(t, "Rae", out[0].Name)
		require.NoError(
			t,
			conn.Update("users").
				Where("name", "Rae").
				Returning("id", "name").SetMap(map[string]any{"name": "Rae2"}).Get(ctx, &out),
		)
		assert.Equal(t, "Rae2", out[0].Name)
		require.NoError(t, conn.Delete("users").Where("name", "Rae2").Returning("id", "name").Get(ctx, &out))
		assert.Len(t, out, 1)
		assert.ErrorIs(t, conn.Delete("users").Where("id", 1).Get(ctx, &out), db.ErrInvalidArgs)
	})
	t.Run("not bound", func(t *testing.T) {
		_, err := db.Use(db.SQLite).Insert("users").Values(map[string]any{"a": 1}).Exec(ctx)
		assert.Equal(t, db.ErrNotBound, err)
		assert.Equal(t, db.ErrNotBound, db.Use(db.SQLite).Truncate(ctx, "users"))
	})
}

func TestSQLitePagination(t *testing.T) {
	conn := newDB(t)
	users := func() *db.SelectBuilder { return conn.Select().From("users").OrderBy("id") }

	t.Run("length aware", func(t *testing.T) {
		var us []User
		p, err := users().Paginate(ctx, 2, 3, &us)
		require.NoError(t, err)
		assert.Equal(t, db.Page{Total: 4, PerPage: 3, CurrentPage: 2, LastPage: 2, From: 4, To: 4}, p)
		assert.Equal(t, []string{"Di"}, names(us))
		p, err = users().Paginate(ctx, 1, 3, &us)
		require.NoError(t, err)
		assert.Equal(t, db.Page{Total: 4, PerPage: 3, CurrentPage: 1, LastPage: 2, From: 1, To: 3}, p)
		p, err = users().Where("id", 999).Paginate(ctx, 1, 3, &us)
		require.NoError(t, err)
		assert.Equal(t, db.Page{Total: 0, PerPage: 3, CurrentPage: 1, LastPage: 1}, p)
		assert.Empty(t, us)
		_, err = users().Paginate(ctx, 1, 0, &us)
		assert.ErrorIs(t, err, db.ErrInvalidArgs)
	})
	t.Run("simple", func(t *testing.T) {
		var us []User
		p, err := users().SimplePaginate(ctx, 1, 3, &us)
		require.NoError(t, err)
		assert.Equal(t, db.SimplePage{PerPage: 3, CurrentPage: 1, HasMore: true}, p)
		assert.Equal(t, []string{"Ann", "Bob", "Cy"}, names(us))
		p, err = users().SimplePaginate(ctx, 2, 3, &us)
		require.NoError(t, err)
		assert.False(t, p.HasMore)
		assert.Equal(t, []string{"Di"}, names(us))
	})
	t.Run("cursor walks every row once", func(t *testing.T) {
		var seen []string
		cursor := ""
		for range 5 {
			var us []User
			p, err := conn.Select().From("users").CursorPaginate(ctx, 3, cursor, &us)
			require.NoError(t, err)
			seen = append(seen, names(us)...)
			if !p.HasMore {
				break
			}
			cursor = p.NextCursor
		}
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, seen)
	})
	t.Run("cursor descending on maps", func(t *testing.T) {
		var rows []map[string]any
		p, err := conn.Select().From("users").CursorPaginate(ctx, 2, "", &rows, db.CursorOptions{Column: "id", Desc: true})
		require.NoError(t, err)
		assert.Equal(t, int64(4), rows[0]["id"])
		assert.True(t, p.HasMore)
		p, err = conn.Select().From("users").
			CursorPaginate(ctx, 2, p.NextCursor, &rows, db.CursorOptions{Column: "id", Desc: true})
		require.NoError(t, err)
		assert.Equal(t, int64(2), rows[0]["id"])
		assert.False(t, p.HasMore)
	})
	t.Run("malformed cursor", func(t *testing.T) {
		var us []User
		for _, c := range []string{"!!!", "bm90LWpzb24"} {
			if _, err := conn.Select().From("users").CursorPaginate(ctx, 2, c, &us); !errors.Is(err, db.ErrInvalidArgs) {
				t.Errorf("cursor %q: %v", c, err)
			}
		}
	})
}

func TestSQLiteChunkingAndStreaming(t *testing.T) {
	conn := newDB(t)

	t.Run("chunk needs order and walks pages", func(t *testing.T) {
		var us []User
		assert.Equal(
			t,
			db.ErrNoOrder,
			conn.Select().From("users").Chunk(ctx, 2, &us, func() (bool, error) { return true, nil }),
		)
		var pages [][]string
		require.NoError(t, conn.Select().From("users").OrderBy("id").Chunk(ctx, 3, &us, func() (bool, error) {
			pages = append(pages, names(us))
			return true, nil
		}))
		assert.Equal(t, [][]string{{"Ann", "Bob", "Cy"}, {"Di"}}, pages)
	})
	t.Run("chunk stops early and propagates errors", func(t *testing.T) {
		calls := 0
		var us []User
		require.NoError(
			t,
			conn.Select().From("users").
				OrderBy("id").
				Chunk(ctx, 1, &us, func() (bool, error) { calls++; return calls < 2, nil }),
		)
		assert.Equal(t, 2, calls)
		boom := errors.New("boom")
		assert.Equal(
			t,
			boom,
			conn.Select().From("users").OrderBy("id").Chunk(ctx, 1, &us, func() (bool, error) { return true, boom }),
		)
	})
	t.Run("chunk by id survives deletes made by the callback", func(t *testing.T) {
		var total int
		require.NoError(
			t,
			db.As[map[string]any](conn.Select().From("posts")).ChunkByID(ctx, 2, func(rows []map[string]any) (bool, error) {
				total += len(rows)
				for _, r := range rows {
					if _, err := conn.Delete("posts").Where("id", r["id"]).Exec(ctx); err != nil {
						return false, err
					}
				}
				return true, nil
			}),
		)
		assert.Equal(t, 4, total)
		c, _ := conn.Select().From("posts").Count(ctx)
		assert.Equal(t, int64(0), c)
	})
	t.Run("typed chunk", func(t *testing.T) {
		var got []string
		require.NoError(t, db.As[User](conn.Select().From("users").OrderBy("id")).Chunk(ctx, 3, func(us []User) (bool, error) {
			got = append(got, names(us)...)
			return true, nil
		}))
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, got)
	})
	t.Run("each", func(t *testing.T) {
		var u User
		var got []string
		require.NoError(
			t,
			conn.Select().From("users").OrderBy("id").Each(ctx, &u, func() error { got = append(got, u.Name); return nil }),
		)
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, got)
		stop := errors.New("stop")
		assert.Equal(t, stop, conn.Select().From("users").Each(ctx, &u, func() error { return stop }))
		assert.Error(t, conn.Select().From("users").Each(ctx, u, func() error { return nil })) // non-pointer
	})
	t.Run("stream", func(t *testing.T) {
		var got []string
		for u, err := range db.Stream[User](ctx, conn.Select().From("users").OrderBy("id")) {
			require.NoError(t, err)
			got = append(got, u.Name)
			if u.Name == "Bob" {
				break // early exit must close the cursor
			}
		}
		assert.Equal(t, []string{"Ann", "Bob"}, got)
		c, err := conn.Select().From("users").Count(ctx) // single connection: fails if the cursor leaked
		require.NoError(t, err)
		assert.Equal(t, int64(4), c)

		var gotErr error
		for _, err := range db.Stream[User](ctx, conn.Select().From("nope")) {
			gotErr = err
		}
		require.Error(t, gotErr)
		for m, err := range db.Stream[map[string]any](ctx, conn.Select("name").From("users").Where("id", 1)) {
			require.NoError(t, err)
			assert.Equal(t, "Ann", m["name"])
		}
	})
}

func TestSQLiteTransactions(t *testing.T) {
	count := func(conn *db.DB) int64 {
		n, err := conn.Select().From("users").Count(ctx)
		require.NoError(t, err)
		return n
	}
	t.Run("commit", func(t *testing.T) {
		conn := newDB(t)
		require.NoError(t, conn.Transaction(ctx, func(tx *db.DB) error {
			_, err := tx.Insert("users").Values(map[string]any{"name": "T", "email": "t@x"}).Exec(ctx)
			return err
		}))
		assert.Equal(t, int64(5), count(conn))
	})
	t.Run("rollback on error", func(t *testing.T) {
		conn := newDB(t)
		boom := errors.New("boom")
		err := conn.Transaction(ctx, func(tx *db.DB) error {
			if _, err := tx.Insert("users").Values(map[string]any{"name": "T", "email": "t@x"}).Exec(ctx); err != nil {
				return err
			}
			return boom
		})
		assert.Equal(t, boom, err)
		assert.Equal(t, int64(4), count(conn))
	})
	t.Run("rollback on panic and re-panic", func(t *testing.T) {
		conn := newDB(t)
		func() {
			defer func() { assert.Equal(t, any("kaboom"), recover()) }()
			_ = conn.Transaction(ctx, func(tx *db.DB) error {
				_, _ = tx.Insert("users").Values(map[string]any{"name": "T", "email": "t@x"}).Exec(ctx)
				panic("kaboom")
			})
			t.Error("panic should propagate")
		}()
		assert.Equal(t, int64(4), count(conn)) // rolled back, and the connection is usable again
	})
	t.Run("nested savepoints roll back independently", func(t *testing.T) {
		conn := newDB(t)
		boom := errors.New("inner")
		require.NoError(t, conn.Transaction(ctx, func(tx *db.DB) error {
			if _, err := tx.Insert("users").Values(map[string]any{"name": "Outer", "email": "o@x"}).Exec(ctx); err != nil {
				return err
			}
			err := tx.Transaction(ctx, func(inner *db.DB) error {
				if _, err := inner.Insert("users").Values(map[string]any{"name": "Inner", "email": "i@x"}).Exec(ctx); err != nil {
					return err
				}
				return boom
			})
			assert.Equal(t, boom, err)
			// a second savepoint that succeeds
			return tx.Transaction(ctx, func(inner *db.DB) error {
				_, err := inner.Insert("users").Values(map[string]any{"name": "Inner2", "email": "i2@x"}).Exec(ctx)
				return err
			})
		}))
		var got []string
		require.NoError(t, conn.Select().From("users").Where("id", ">", 4).OrderBy("id").Pluck(ctx, "name", &got))
		assert.Equal(t, []string{"Outer", "Inner2"}, got)
	})
	t.Run("using an existing *sql.Tx", func(t *testing.T) {
		sdb, _ := sql.Open("sqlite", ":memory:")
		sdb.SetMaxOpenConns(1)
		defer func() { _ = sdb.Close() }()
		_, err := sdb.Exec(schema)
		require.NoError(t, err)
		stx, err := sdb.Begin()
		require.NoError(t, err)
		conn := db.New(stx, db.SQLite)
		boom := errors.New("x")
		assert.Equal(t, boom, conn.Transaction(ctx, func(tx *db.DB) error {
			_, _ = tx.Insert("users").Values(map[string]any{"name": "S", "email": "s@x"}).Exec(ctx)
			return boom
		}))
		assert.Equal(t, int64(4), count(conn)) // savepoint rolled back
		require.NoError(t, stx.Rollback())
	})
	t.Run("connection that cannot begin", func(t *testing.T) {
		conn := db.New(noBegin{newDB(t)}, db.SQLite)
		assert.Equal(t, db.ErrNoTransaction, conn.Transaction(ctx, func(*db.DB) error { return nil }))
	})
}

// noBegin hides BeginTx so the DB looks like a bare Conn.
type noBegin struct{ conn *db.DB }

func (n noBegin) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return nil, errors.New("unused")
}
func (n noBegin) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, errors.New("unused")
}

func TestSQLiteHooksAndRawAccess(t *testing.T) {
	t.Run("args are redacted by default", func(t *testing.T) {
		var evs []db.QueryEvent
		conn := newDB(t, db.WithHook(func(_ context.Context, e db.QueryEvent) { evs = append(evs, e) }))
		var us []User
		require.NoError(t, conn.Select().From("users").Where("email", "ann@x").Get(ctx, &us))
		assert.Len(t, evs, 1)
		assert.Equal(t, 1, evs[0].NumArgs)
		assert.Nil(t, evs[0].Args)
		assert.NotContains(t, evs[0].SQL, "ann@x")
		assert.GreaterOrEqual(t, int64(evs[0].Duration), int64(0))
	})
	t.Run("args logged on request, errors reported", func(t *testing.T) {
		var evs []db.QueryEvent
		conn := newDB(
			t,
			db.WithLogArgs(),
			db.WithHook(func(_ context.Context, e db.QueryEvent) { evs = append(evs, e) }),
		)
		_, err := conn.Exec(ctx, "insert into nope values (?)", 1)
		require.Error(t, err)
		assert.Equal(t, []any{1}, evs[0].Args)
		assert.Error(t, evs[0].Err)
	})
	t.Run("raw query and exec", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Exec(ctx, "update users set active = ? where id = ?", 0, 1)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		var us []User
		require.NoError(t, conn.Query(ctx, &us, "select * from users where active = ? order by id", 0))
		assert.Equal(t, []string{"Ann", "Bob"}, names(us))
	})
	t.Run("prefix option and dialect accessor", func(t *testing.T) {
		sdb, _ := sql.Open("sqlite", ":memory:")
		sdb.SetMaxOpenConns(1)
		defer func() { _ = sdb.Close() }()
		_, err := sdb.Exec(`create table pre_t (id integer primary key, v text); insert into pre_t(v) values ('x')`)
		require.NoError(t, err)
		conn := db.New(sdb, db.SQLite, db.WithPrefix("pre_"))
		assert.Equal(t, db.SQLite, conn.Dialect())
		var v string
		require.NoError(t, conn.Select().From("t").Where("t.id", 1).Value(ctx, "v", &v))
		assert.Equal(t, "x", v)
	})
}

func TestSQLiteTypedQueries(t *testing.T) {
	conn := newDB(t)
	users := func() *db.SelectBuilder { return conn.Select().From("users").OrderBy("id") }

	t.Run("get first find sole", func(t *testing.T) {
		us, err := db.As[User](users()).Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, names(us))
		u, err := db.As[User](users().Where("name", "Bob")).First(ctx)
		require.NoError(t, err)
		assert.Equal(t, "Bob", u.Name)
		u, err = db.As[User](conn.Select().From("users")).Find(ctx, 3)
		require.NoError(t, err)
		assert.Equal(t, "Cy", u.Name)
		u, err = db.As[User](users().Where("name", "Ann")).Sole(ctx)
		require.NoError(t, err)
		assert.Equal(t, "Ann", u.Name)
		_, err = db.As[User](users().Where("age", ">", 0)).Sole(ctx)
		assert.Equal(t, db.ErrMultipleRecords, err)
		_, err = db.As[User](users().Where("name", "nobody")).First(ctx)
		require.ErrorIs(t, err, db.ErrNotFound)
		// the handle gives the same builder back for further chaining
		b := users()
		assert.Same(t, db.As[User](b).Builder(), b)
	})
	t.Run("maps and scalars", func(t *testing.T) {
		m, err := db.As[map[string]any](conn.Select("name").From("users").Where("id", 1)).First(ctx)
		require.NoError(t, err)
		assert.Equal(t, "Ann", m["name"])
		ids, err := db.As[int64](conn.Select("id").From("users").OrderBy("id")).Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, []int64{1, 2, 3, 4}, ids)

		name, err := db.ValueAs[string](ctx, conn.Select().From("users").Where("id", 2), "name")
		require.NoError(t, err)
		assert.Equal(t, "Bob", name)
		age, err := db.ValueAs[int](ctx, conn.Select().From("users").Where("id", 4), "age") // NULL -> zero
		require.NoError(t, err)
		assert.Equal(t, 0, age)
		ns, err := db.PluckAs[string](ctx, users().Where("active", 1), "name")
		require.NoError(t, err)
		assert.Equal(t, []string{"Ann", "Cy", "Di"}, ns)
		var mn, mx int
		err = conn.Select().From("users").Min(ctx, "age", &mn)
		require.NoError(t, err)
		err = conn.Select().From("users").Max(ctx, "age", &mx)
		require.NoError(t, err)
		assert.Equal(t, [2]int{25, 41}, [2]int{mn, mx})
		sum, err := conn.Select().From("users").Sum(ctx, "age")
		require.NoError(t, err)
		assert.Equal(t, 96.0, sum)
	})
	t.Run("pagination carries its rows", func(t *testing.T) {
		p, err := db.As[User](users()).Paginate(ctx, 2, 3)
		require.NoError(t, err)
		assert.Equal(t, int64(4), p.Total) // promoted from the embedded Page
		assert.Equal(t, 2, p.LastPage)
		assert.Equal(t, []string{"Di"}, names(p.Items))
		sp, err := db.As[User](users()).SimplePaginate(ctx, 1, 3)
		require.NoError(t, err)
		assert.True(t, sp.HasMore)
		assert.Len(t, sp.Items, 3)

		var seen []string
		cursor := ""
		for range 5 {
			cp, err := db.As[User](conn.Select().From("users")).CursorPaginate(ctx, 3, cursor)
			require.NoError(t, err)
			seen = append(seen, names(cp.Items)...)
			if !cp.HasMore {
				break
			}
			cursor = cp.NextCursor
		}
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, seen)
	})
	t.Run("chunk each stream", func(t *testing.T) {
		var pages []int
		require.NoError(t, db.As[User](users()).Chunk(ctx, 3, func(us []User) (bool, error) {
			pages = append(pages, len(us))
			return true, nil
		}))
		assert.Equal(t, []int{3, 1}, pages)
		var got []string
		require.NoError(t, db.As[User](users()).ChunkByID(ctx, 2, func(us []User) (bool, error) {
			got = append(got, names(us)...)
			return true, nil
		}))
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, got)
		got = nil
		require.NoError(t, db.As[User](users()).Each(ctx, func(u User) error { got = append(got, u.Name); return nil }))
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, got)
		got = nil
		for u, err := range db.As[User](users()).Stream(ctx) {
			require.NoError(t, err)
			got = append(got, u.Name)
			if u.Name == "Bob" {
				break
			}
		}
		assert.Equal(t, []string{"Ann", "Bob"}, got)
	})
	t.Run("returning gives typed rows", func(t *testing.T) {
		var rows []Member
		err := conn.Insert("users").Rows(Member{Name: "Rae", Email: ptr("rae@x")}).Returning("*").Get(ctx, &rows)
		require.NoError(t, err)
		assert.Len(t, rows, 1)
		assert.Equal(t, "Rae", rows[0].Name)
		assert.Greater(t, rows[0].ID, int64(4))
		err = conn.Update("users").Where("name", "Rae").SetRow(Member{Bio: "hello"}).Returning("*").Get(ctx, &rows)
		require.NoError(t, err)
		assert.Equal(t, "hello", rows[0].Bio)
		assert.Equal(t, "Rae", rows[0].Name) // untouched columns survive
		err = conn.Delete("users").Where("name", "Rae").Returning("*").Get(ctx, &rows)
		require.NoError(t, err)
		assert.Len(t, rows, 1)
	})
}

func TestSQLiteStructWrites(t *testing.T) {
	t.Run("insert variants", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Insert("users").Rows(Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Insert("users").Rows().Exec(ctx) // nothing to insert
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		id, err := conn.Insert("users").Rows(Member{Name: "Gus", Email: ptr("gus@x")}).ExecGetID(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(7), id)
		n, err = conn.Insert("users").Rows(Member{Name: "Dup", Email: ptr("ann@x")}).Ignore().Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		// pointers to structs work too
		n, err = conn.Insert("users").Rows(&Member{Name: "Hal", Email: ptr("hal@x")}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		// the column default applies because omitted fields are not written
		var active bool
		require.NoError(t, conn.Select().From("users").Where("name", "Eve").Value(ctx, "active", &active))
		assert.True(t, active)
	})
	t.Run("upsert", func(t *testing.T) {
		conn := newDB(t)
		_, err := conn.Insert("users").Rows([]Member{
			{Name: "Ann!", Email: ptr("ann@x"), Age: ptr(31)},
			{Name: "New", Email: ptr("new@x"), Age: ptr(1)},
		}).Upsert([]string{"email"}, []string{"name", "age"}).Exec(ctx)
		require.NoError(t, err)
		var name string
		require.NoError(t, conn.Select().From("users").Where("email", "ann@x").Value(ctx, "name", &name))
		assert.Equal(t, "Ann!", name)
		c, _ := conn.Select().From("users").Count(ctx)
		assert.Equal(t, int64(5), c)
	})
	t.Run("update writes only what is set", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Update("users").Where("id", 1).SetRow(Member{Name: "Annie"}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		var u User
		require.NoError(t, conn.Select().From("users").Find(ctx, 1, &u))
		assert.Equal(t, "Annie", u.Name)
		assert.True(t, u.Age != nil && *u.Age == 30) // not overwritten by a zero value

		// a pointer to zero is "set": it writes 0, where a plain zero would be skipped
		_, err = conn.Update("users").Where("id", 1).SetRow(Member{Age: ptr(0)}).Exec(ctx)
		require.NoError(t, err)
		require.NoError(t, conn.Select().From("users").Find(ctx, 1, &u))
		assert.True(t, u.Age != nil && *u.Age == 0)

		_, err = conn.Update("users").Where("id", 1).SetRow(Member{}).Exec(ctx)
		assert.Equal(t, db.ErrNoValues, err)
		_, err = conn.Update("users").SetRow(Member{Name: "x"}).Exec(ctx)
		assert.Equal(t, db.ErrNoWhere, err)
	})
	t.Run("bad input is an error", func(t *testing.T) {
		conn := newDB(t)
		_, err := conn.Insert("users").Rows(5).Exec(ctx)
		require.ErrorIs(t, err, db.ErrInvalidArgs)
		_, err = conn.Update("users").Where("id", 1).SetRow(map[string]any{"a": 1}).Exec(ctx)
		assert.ErrorIs(t, err, db.ErrInvalidArgs)
	})
}

func TestSQLiteFromTakesTheTableFromTheType(t *testing.T) {
	conn := newDB(t)

	t.Run("reads", func(t *testing.T) {
		us, err := db.From[User](conn).Where("active", 1).OrderBy("id").Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"Ann", "Cy", "Di"}, names(us))
		u, err := db.From[User](conn).Find(ctx, 2)
		require.NoError(t, err)
		assert.Equal(t, "Bob", u.Name)
		n, err := db.From[User](conn).Where("age", ">", 26).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		ok, err := db.From[User](conn).Where("name", "Ann").Exists(ctx)
		require.NoError(t, err)
		assert.True(t, ok)
		gone, err := db.From[User](conn).Where("name", "zzz").DoesntExist(ctx)
		require.NoError(t, err)
		assert.True(t, gone)
		sum, err := db.From[Post](conn).Sum(ctx, "views") // pointer-receiver TableName
		require.NoError(t, err)
		assert.Equal(t, 23.0, sum)
		avg, err := db.From[Post](conn).Avg(ctx, "views")
		require.NoError(t, err)
		assert.Equal(t, 5.75, avg)
		p, err := db.From[User](conn).OrderBy("id").Paginate(ctx, 2, 3)
		require.NoError(t, err)
		assert.Equal(t, [2]any{int64(4), "Di"}, [2]any{p.Total, names(p.Items)[0]})
	})
	t.Run("chain helpers and Apply reach any builder method", func(t *testing.T) {
		us, err := db.From[User](conn).
			When(true, func(q *db.Query[User]) { q.Where("active", 1) }).
			Apply(func(b *db.SelectBuilder) { b.WhereYear("created_at", 2024) }).
			OrderBy("id").
			Get(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"Ann"}, names(us))
	})
	t.Run("writes need no table name or builder", func(t *testing.T) {
		n, err := conn.Insert("users").Rows(Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		id, err := conn.Insert("users").Rows(Member{Name: "Gus", Email: ptr("gus@x")}).ExecGetID(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(7), id)
		n, err = conn.Insert("users").Rows(Member{Name: "Dup", Email: ptr("ann@x")}).Ignore().Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		_, err = conn.Insert("users").Rows([]Member{{Name: "Ann!", Email: ptr("ann@x")}}).Upsert([]string{"email"}, []string{"name"}).Exec(ctx)
		require.NoError(t, err)

		n, err = conn.Update("users").Where("id", 1).SetRow(Member{Bio: "set"}).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		n, err = conn.Update("users").Where("id", 1).SetMap(map[string]any{"bio": nil}).Exec(ctx) // explicit NULL
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		_, err = conn.Update("users").SetRow(Member{Name: "x"}).Exec(ctx)
		assert.Equal(t, db.ErrNoWhere, err)

		n, err = conn.Delete("users").Where("name", "Fay").Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		_, err = conn.Delete("users").Exec(ctx)
		assert.Equal(t, db.ErrNoWhere, err)

		var rows []Member
		err = conn.Insert("users").Rows(Member{Name: "Rae", Email: ptr("rae@x")}).Returning("*").Get(ctx, &rows)
		require.NoError(t, err)
		assert.Equal(t, "Rae", rows[0].Name)

		require.NoError(t, conn.Truncate(ctx, "posts"))
		c, err := db.From[Post](conn).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), c)
		pid, err := conn.Insert("posts").Rows(Post{Title: "fresh"}).ExecGetID(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), pid)
	})
	t.Run("a type without TableName fails clearly", func(t *testing.T) {
		_, err := db.From[Untitled](conn).Get(ctx)
		require.ErrorIs(t, err, db.ErrInvalidArgs)
		_, err = db.From[Untitled](conn).Where("id", 1).Count(ctx)
		require.ErrorIs(t, err, db.ErrInvalidArgs)
		_, err = db.TableName[Untitled]()
		require.ErrorIs(t, err, db.ErrInvalidArgs)
		// As still works for types without one: the table is given explicitly
		us, err := db.As[Untitled](conn.Select("id").From("users").OrderBy("id")).Get(ctx)
		require.NoError(t, err)
		total, err := conn.Select().From("users").Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, total, int64(len(us)))
	})
}

func TestSQLiteCTE(t *testing.T) {
	conn := newDB(t)
	active := func() *db.SelectBuilder { return conn.Select().From("users").Where("active", true) }

	t.Run("plain", func(t *testing.T) {
		assert.Equal(
			t,
			[]string{"Ann", "Cy", "Di"},
			pluckNames(t, conn.Select().From("au").With("au", active()).OrderBy("name")),
		)
	})
	t.Run("recursive series", func(t *testing.T) {
		anchor := conn.Select().From("users").SelectRaw("1 as n").Where("id", 1)
		step := conn.Select().From("seq").SelectRaw("n + 1").Where("n", "<", 5)
		var got []int64
		require.NoError(
			t,
			conn.Select().From("seq").WithRecursive("seq", anchor.UnionAll(step), "n").OrderBy("n").Pluck(ctx, "n", &got),
		)
		assert.Equal(t, []int64{1, 2, 3, 4, 5}, got)
	})
	t.Run("inside where in", func(t *testing.T) {
		var got []string
		sub := conn.Select("id").From("au").With("au", active())
		require.NoError(t, conn.Select().From("posts").WhereIn("user_id", sub).OrderBy("title").Pluck(ctx, "title", &got))
		assert.Equal(t, []string{"p1", "p2", "p4"}, got)
	})
	t.Run("with an outer union", func(t *testing.T) {
		q := conn.Select("name").From("au").With("au", active()).Where("name", "Ann").
			UnionAll(conn.Select("name").From("users").Where("name", "Bob")).OrderBy("name")
		assert.Equal(t, []string{"Ann", "Bob"}, pluckNames(t, q))
	})
	t.Run("count and exists", func(t *testing.T) {
		n, err := conn.Select().From("au").With("au", active()).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(3), n)
		ok, err := conn.Select().From("au").With("au", active()).Where("name", "Bob").Exists(ctx)
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("insert using", func(t *testing.T) {
		sub := conn.Select("name").From("inactive").With("inactive", conn.Select().From("users").Where("active", false))
		n, err := conn.Insert("users").Using([]string{"name"}, sub).Exec(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		c, err := conn.Select().From("users").Where("name", "Bob").Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), c)
	})
}
