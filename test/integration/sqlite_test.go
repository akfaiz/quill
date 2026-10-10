package integration

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/akfaiz/quill/db"
	_ "modernc.org/sqlite"
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
	if err != nil {
		t.Fatal(err)
	}
	sdb.SetMaxOpenConns(1) // each :memory: connection is its own database
	t.Cleanup(func() { sdb.Close() })
	if _, err := sdb.Exec(schema); err != nil {
		t.Fatal(err)
	}
	return db.New(sdb, db.SQLite, opts...)
}

func TestSQLiteScanning(t *testing.T) {
	conn := newDB(t)

	t.Run("structs with NULL guard and pointer fields", func(t *testing.T) {
		var us []User
		noErr(t, conn.Table("users").OrderBy("id").Get(ctx, &us))
		eq(t, names(us), []string{"Ann", "Bob", "Cy", "Di"})
		eq(t, us[2].Bio, "hello")
		eq(t, us[0].Bio, "") // NULL -> zero value
		if us[3].Age != nil || us[0].Age == nil || *us[0].Age != 30 {
			t.Errorf("pointer NULL handling wrong: %+v %+v", us[3].Age, us[0].Age)
		}
		eq(t, us[1].Active, false)
		eq(t, us[0].Score, 1.5)
	})
	t.Run("pointer elements", func(t *testing.T) {
		var us []*User
		noErr(t, conn.Table("users").OrderBy("id").Limit(2).Get(ctx, &us))
		eq(t, len(us), 2)
		eq(t, us[1].Name, "Bob")
	})
	t.Run("maps convert bytes and keep NULL as nil", func(t *testing.T) {
		var rows []map[string]any
		noErr(t, conn.Table("users").Select("name", "age").OrderBy("id").Get(ctx, &rows))
		eq(t, rows[0]["name"], "Ann")
		eq(t, rows[0]["age"], int64(30))
		eq(t, rows[3]["age"], nil)
	})
	t.Run("embedded structs, tag, conn:- and case-insensitive fallback", func(t *testing.T) {
		var es []Embedded
		noErr(t, conn.Table("users").Select("id", "NAME", "email").OrderBy("id").Limit(1).Get(ctx, &es))
		eq(t, es[0], Embedded{Base: Base{1}, Name: "Ann", UserEmail: "ann@x"})
	})
	t.Run("unknown columns are ignored", func(t *testing.T) {
		var us []User
		noErr(t, conn.Table("users").Select("id", "name", "meta").Limit(1).OrderBy("id").Get(ctx, &us))
		eq(t, us[0].Name, "Ann")
	})
	t.Run("empty result is an empty non-nil slice", func(t *testing.T) {
		var us []User
		noErr(t, conn.Table("users").Where("id", 999).Get(ctx, &us))
		if us == nil || len(us) != 0 {
			t.Errorf("want empty non-nil slice, got %#v", us)
		}
	})
	t.Run("bad destinations", func(t *testing.T) {
		var u User
		if err := conn.Table("users").Get(ctx, &u); !errors.Is(err, db.ErrInvalidArgs) {
			t.Errorf("non-slice dest: %v", err)
		}
		if err := conn.Table("users").Get(ctx, nil); !errors.Is(err, db.ErrInvalidArgs) {
			t.Errorf("nil dest: %v", err)
		}
		var names []string
		if err := conn.Table("users").Select("id", "name").Get(ctx, &names); err == nil {
			t.Error("scalar dest with two columns should fail")
		}
	})
	t.Run("generic helpers", func(t *testing.T) {
		us, err := db.As[User](conn.Table("users").OrderBy("id")).Get(ctx)
		noErr(t, err)
		eq(t, len(us), 4)
		u, err := db.As[User](conn.Table("users").Where("name", "Cy")).First(ctx)
		noErr(t, err)
		eq(t, u.Age != nil && *u.Age == 41, true)
		_, err = db.As[User](conn.Table("users").Where("name", "nobody")).First(ctx)
		eq(t, errors.Is(err, db.ErrNotFound), true)
	})
}

func TestSQLiteReads(t *testing.T) {
	conn := newDB(t)
	users := func() *db.Builder { return conn.Table("users") }

	t.Run("first find sole", func(t *testing.T) {
		var u User
		noErr(t, users().OrderBy("age", "desc").First(ctx, &u))
		eq(t, u.Name, "Cy")
		noErr(t, users().Find(ctx, 2, &u))
		eq(t, u.Name, "Bob")
		eq(t, errors.Is(users().Find(ctx, 99, &u), sql.ErrNoRows), true)
		noErr(t, users().Where("name", "Ann").Sole(ctx, &u))
		eq(t, users().Where("age", ">", 0).Sole(ctx, &u), db.ErrMultipleRecords)
		eq(t, users().Where("name", "zzz").Sole(ctx, &u), db.ErrNotFound)
	})
	t.Run("value pluck pluckmap", func(t *testing.T) {
		var age int
		noErr(t, users().Where("name", "Bob").Value(ctx, "age", &age))
		eq(t, age, 25)
		var ns []string
		noErr(t, users().Where("active", 1).OrderBy("name").Pluck(ctx, "name", &ns))
		eq(t, ns, []string{"Ann", "Cy", "Di"})
		var m map[int64]string
		noErr(t, users().PluckMap(ctx, "name", "id", &m))
		eq(t, m, map[int64]string{1: "Ann", 2: "Bob", 3: "Cy", 4: "Di"})
		var nullable []int // NULL age -> 0
		noErr(t, users().OrderBy("id").Pluck(ctx, "age", &nullable))
		eq(t, nullable, []int{30, 25, 41, 0})
		if err := users().PluckMap(ctx, "name", "id", &ns); !errors.Is(err, db.ErrInvalidArgs) {
			t.Errorf("PluckMap non-map: %v", err)
		}
	})
	t.Run("exists", func(t *testing.T) {
		ok, err := users().Where("name", "Ann").Exists(ctx)
		noErr(t, err)
		eq(t, ok, true)
		no, err := users().Where("name", "zzz").DoesntExist(ctx)
		noErr(t, err)
		eq(t, no, true)
	})
	t.Run("aggregates", func(t *testing.T) {
		n, err := users().Count(ctx)
		noErr(t, err)
		eq(t, n, int64(4))
		n, err = users().Count(ctx, "age") // count(col) skips NULL
		noErr(t, err)
		eq(t, n, int64(3))
		sum, err := users().Sum(ctx, "age")
		noErr(t, err)
		eq(t, sum, 96.0)
		avg, err := users().Avg(ctx, "age")
		noErr(t, err)
		eq(t, avg, 32.0)
		var mn, mx int
		noErr(t, users().Min(ctx, "age", &mn))
		noErr(t, users().Max(ctx, "age", &mx))
		eq(t, [2]int{mn, mx}, [2]int{25, 41})
		empty, err := users().Where("id", 99).Sum(ctx, "age") // NULL -> 0
		noErr(t, err)
		eq(t, empty, 0.0)
		// ordering must not break an aggregate
		n, err = users().OrderBy("name").Count(ctx)
		noErr(t, err)
		eq(t, n, int64(4))
	})
	t.Run("aggregates over grouped, distinct and limited queries are wrapped", func(t *testing.T) {
		n, err := conn.Table("posts").GroupBy("user_id").Count(ctx)
		noErr(t, err)
		eq(t, n, int64(3)) // 3 groups, not 4 rows
		n, err = conn.Table("posts").Select("user_id").Distinct().Count(ctx)
		noErr(t, err)
		eq(t, n, int64(3))
		n, err = conn.Table("users").OrderBy("id").Limit(2).Count(ctx)
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("posts").Join("users", "users.id", "posts.user_id").Where("posts.views", ">", 0).Count(ctx)
		noErr(t, err)
		eq(t, n, int64(4))
		total, err := conn.Table("posts").Limit(3).OrderBy("id").Sum(ctx, "posts.views") // qualified col in wrapped query
		noErr(t, err)
		eq(t, total, 22.0)
	})
	t.Run("not bound and cancelled context", func(t *testing.T) {
		var us []User
		eq(t, db.Use(db.SQLite).Table("users").Get(ctx, &us), db.ErrNotBound)
		_, err := db.Use(db.SQLite).Table("users").Count(ctx)
		eq(t, err, db.ErrNotBound)
		c, cancel := context.WithCancel(ctx)
		cancel()
		if err := users().Get(c, &us); err == nil {
			t.Error("cancelled context should fail")
		}
	})
	t.Run("builder errors surface from executors", func(t *testing.T) {
		var us []User
		if err := users().Where("a", "bogus", 1).Get(ctx, &us); !errors.Is(err, db.ErrInvalidOperator) {
			t.Errorf("got %v", err)
		}
	})
}

func TestSQLiteQueryShapesExecute(t *testing.T) {
	conn := newDB(t)
	pluck := func(t *testing.T, b *db.Builder) []string {
		t.Helper()
		var out []string
		noErr(t, b.Pluck(ctx, "name", &out))
		return out
	}
	users := func() *db.Builder { return conn.Table("users").OrderBy("users.id") }

	cases := []struct {
		name string
		q    *db.Builder
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
		{"group", users().Where("active", 1).WhereGroup(func(q *db.Builder) { q.Where("age", ">", 35).OrWhereNull("age") }), []string{"Cy", "Di"}},
		{"not", users().WhereNot("active", 1), []string{"Bob"}},
		{"exists subquery", users().WhereExists(conn.Table("posts").SelectRaw("1").WhereColumn("posts.user_id", "users.id").Where("views", ">", 6)), []string{"Ann", "Bob"}},
		{"in subquery", users().WhereIn("id", conn.Table("posts").Select("user_id").Where("published", 1)), []string{"Ann", "Bob", "Cy"}},
		{"scalar subquery compare", users().Where("age", ">", conn.Table("users").SelectRaw("avg(age)")), []string{"Cy"}},
		{"any / all / none", users().WhereAny([]string{"name", "email"}, "like", "b%"), []string{"Bob"}},
		{"date parts", users().WhereYear("created_at", 2024).WhereMonth("created_at", 3).WhereDay("created_at", ">", 7), []string{"Bob"}},
		{"date and time", users().WhereDate("created_at", "2024-03-07").WhereTime("created_at", ">=", "09:00:00"), []string{"Ann"}},
		{"row values", users().WhereRowValues([]string{"id", "age"}, "=", []any{1, 30}), []string{"Ann"}},
		{"raw", users().WhereRaw("length(name) = ?", 2), []string{"Cy", "Di"}},
		{"order latest limit offset", conn.Table("users").Latest("id").Limit(2).Offset(1), []string{"Cy", "Bob"}},
		{"left join with value", users().LeftJoinFunc("posts", func(j *db.JoinClause) { j.On("posts.user_id", "users.id").Where("posts.views", ">", 8) }).WhereNull("posts.id"), []string{"Bob", "Cy", "Di"}},
		{"join sub", users().JoinSub(conn.Table("posts").Select("user_id").SelectRaw("max(views) as mv").GroupBy("user_id"), "m", "m.user_id", "users.id").Where("m.mv", ">", 6), []string{"Ann", "Bob"}},
		{"cross join", conn.Table("users").CrossJoin("posts").Where("users.id", 1).Where("posts.id", 1), []string{"Ann"}},
		{"from sub", conn.NewQuery().FromSub(conn.Table("users").Where("active", 1), "au").OrderBy("name"), []string{"Ann", "Cy", "Di"}},
		{"union", conn.Table("users").Select("name").Where("id", 1).Union(conn.Table("users").Select("name").Where("id", 2)).OrderBy("name", "desc"), []string{"Bob", "Ann"}},
		{"union all with limit", conn.Table("users").Select("name").Where("id", 1).UnionAll(conn.Table("users").Select("name").Where("id", 1)).Limit(5), []string{"Ann", "Ann"}},
		{"shared lock is a no-op on sqlite", users().SharedLock().Limit(1), []string{"Ann"}},
		{"force index", conn.Table("users").ForceIndex("sqlite_autoindex_users_1").Where("email", "cy@x"), []string{"Cy"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pluck(t, tc.q)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			eq(t, got, tc.want)
		})
	}

	// these only need to be valid SQL for the engine
	for name, q := range map[string]*db.Builder{
		"column compare":  conn.Table("posts").WhereColumn("views", ">", "user_id").OrderBy("id"),
		"between columns": conn.Table("users").WhereBetweenColumns("age", "id", "score"),
		"random order":    conn.Table("users").InRandomOrder().Limit(1),
		"group having":    conn.Table("posts").Select("user_id").SelectRaw("sum(views) as v").GroupBy("user_id").Having("v", ">", 6),
	} {
		t.Run("executes: "+name, func(t *testing.T) {
			var rows []map[string]any
			noErr(t, q.Get(ctx, &rows))
		})
	}

	t.Run("select sub value", func(t *testing.T) {
		var rows []map[string]any
		noErr(t, conn.Table("users").Select("name").SelectSub(conn.Table("posts").SelectRaw("count(*)").WhereColumn("posts.user_id", "users.id"), "pc").OrderBy("id").Get(ctx, &rows))
		eq(t, rows[0]["pc"], int64(2))
		eq(t, rows[3]["pc"], int64(0))
	})
	t.Run("lateral join is rejected on sqlite", func(t *testing.T) {
		var rows []map[string]any
		err := conn.Table("users").JoinLateral(conn.Table("posts"), "p").Get(ctx, &rows)
		eq(t, errors.Is(err, db.ErrUnsupported), true)
	})
}

func TestSQLiteJSONExecution(t *testing.T) {
	conn := newDB(t)
	pluck := func(b *db.Builder) []string {
		var out []string
		noErr(t, b.OrderBy("id").Pluck(ctx, "name", &out))
		return out
	}
	t.Run("contains / doesn't contain", func(t *testing.T) {
		eq(t, pluck(conn.Table("users").WhereJsonContains("meta->tags", "b")), []string{"Ann", "Bob"})
		eq(t, pluck(conn.Table("users").WhereJsonContains("meta->tags", "a")), []string{"Ann"})
		eq(t, pluck(conn.Table("users").WhereJsonDoesntContain("meta->tags", "a").WhereNotNull("meta")), []string{"Bob", "Cy"})
	})
	t.Run("contains key / length / selector", func(t *testing.T) {
		eq(t, pluck(conn.Table("users").WhereJsonContainsKey("meta->theme")), []string{"Ann", "Bob"})
		eq(t, pluck(conn.Table("users").WhereJsonLength("meta->tags", ">", 1)), []string{"Ann"})
		eq(t, pluck(conn.Table("users").Where("meta->theme", "light")), []string{"Bob"})
		var themes []string
		noErr(t, conn.Table("users").Where("id", 1).Pluck(ctx, "meta->theme", &themes))
		eq(t, themes, []string{"dark"})
	})
	t.Run("json updates patch in place", func(t *testing.T) {
		n, err := conn.Table("users").Where("id", 1).Update(ctx, map[string]any{"meta->theme": "blue", "meta->flags->beta": true})
		noErr(t, err)
		eq(t, n, int64(1))
		var theme string
		var beta bool
		noErr(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->theme", &theme))
		eq(t, theme, "blue")
		noErr(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->flags->beta", &beta))
		eq(t, beta, true)
		// untouched siblings survive the patch
		eq(t, pluck(conn.Table("users").WhereJsonContains("meta->tags", "a")), []string{"Ann"})
	})
	t.Run("boolean comparison", func(t *testing.T) {
		eq(t, pluck(conn.Table("users").Where("meta->flags->beta", true)), []string{"Ann"})
	})
}

func TestSQLiteWrites(t *testing.T) {
	t.Run("insert variants", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Table("users").Insert(ctx, map[string]any{"name": "Eve", "email": "eve@x"}, map[string]any{"name": "Fay", "email": "fay@x"})
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("users").Insert(ctx) // no rows: no-op
		noErr(t, err)
		eq(t, n, int64(0))
		id, err := conn.Table("users").InsertGetID(ctx, map[string]any{"name": "Gus", "email": "gus@x"})
		noErr(t, err)
		eq(t, id, int64(7))
		n, err = conn.Table("users").InsertOrIgnore(ctx, map[string]any{"name": "Dup", "email": "ann@x"})
		noErr(t, err)
		eq(t, n, int64(0)) // conflicts on unique email
		n, err = conn.Table("posts").InsertUsing(ctx, []string{"user_id", "title"},
			conn.Table("users").Select("id").SelectRaw("'from ' || name").Where("id", "<=", 2))
		noErr(t, err)
		eq(t, n, int64(2))
		var t1 string
		noErr(t, conn.Table("posts").Where("title", "like", "from%").OrderBy("id").Value(ctx, "title", &t1))
		eq(t, t1, "from Ann")
		_, err = conn.Table("users").Insert(ctx, map[string]any{"name": "Ann2", "email": "ann@x"})
		if err == nil {
			t.Error("unique violation should error")
		}
	})
	t.Run("upsert and update-or-insert", func(t *testing.T) {
		conn := newDB(t)
		_, err := conn.Table("users").Upsert(ctx, []map[string]any{
			{"name": "Ann!", "email": "ann@x", "age": 31},
			{"name": "New", "email": "new@x", "age": 1},
		}, []string{"email"}, []string{"name", "age"})
		noErr(t, err)
		var name string
		noErr(t, conn.Table("users").Where("email", "ann@x").Value(ctx, "name", &name))
		eq(t, name, "Ann!")
		c, _ := conn.Table("users").Count(ctx)
		eq(t, c, int64(5))

		ok, err := conn.Table("users").UpdateOrInsert(ctx, map[string]any{"email": "zed@x"}, map[string]any{"name": "Zed"})
		noErr(t, err)
		eq(t, ok, true)
		ok, err = conn.Table("users").UpdateOrInsert(ctx, map[string]any{"email": "zed@x"}, map[string]any{"name": "Zed2"})
		noErr(t, err)
		eq(t, ok, true)
		noErr(t, conn.Table("users").Where("email", "zed@x").Value(ctx, "name", &name))
		eq(t, name, "Zed2")
		c, _ = conn.Table("users").Count(ctx)
		eq(t, c, int64(6))
	})
	t.Run("update increment decrement", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Table("users").Where("active", 1).Update(ctx, map[string]any{"bio": "on"})
		noErr(t, err)
		eq(t, n, int64(3))
		n, err = conn.Table("posts").Where("user_id", 1).Increment(ctx, "views", 5, map[string]any{"title": "bumped"})
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("posts").Where("id", 1).DecrementEach(ctx, map[string]any{"views": 1, "published": 1}, nil)
		noErr(t, err)
		eq(t, n, int64(1))
		var rows []map[string]any
		noErr(t, conn.Table("posts").Where("id", 1).Get(ctx, &rows))
		eq(t, rows[0]["views"], int64(14))
		eq(t, rows[0]["published"], int64(0))
		eq(t, rows[0]["title"], "bumped")
		_, err = conn.Table("users").Update(ctx, map[string]any{"bio": "x"})
		eq(t, err, db.ErrNoWhere)
		n, err = conn.Table("users").AllRows().Update(ctx, map[string]any{"bio": "all"})
		noErr(t, err)
		eq(t, n, int64(4))
	})
	t.Run("decrement, increment each, insert-or-ignore-using, update-from", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Table("posts").Where("id", 1).Decrement(ctx, "views", 3, nil)
		noErr(t, err)
		eq(t, n, int64(1))
		n, err = conn.Table("posts").Where("user_id", 1).IncrementEach(ctx, map[string]any{"views": 1, "published": 1}, nil)
		noErr(t, err)
		eq(t, n, int64(2))
		var v int
		noErr(t, conn.Table("posts").Where("id", 1).Value(ctx, "views", &v))
		eq(t, v, 8) // 10 - 3 + 1

		n, err = conn.Table("users").InsertOrIgnoreUsing(ctx, []string{"name", "email"},
			conn.Table("users").Select("name").SelectRaw("email || 'x'").Where("id", "<=", 2))
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("users").InsertOrIgnoreUsing(ctx, []string{"name", "email"}, // same emails again: ignored
			conn.Table("users").Select("name").SelectRaw("email || 'x'").Where("id", "<=", 2))
		noErr(t, err)
		eq(t, n, int64(0))

		_, err = conn.Table("users").Where("id", 1).UpdateFrom(ctx, map[string]any{"name": "x"})
		eq(t, errors.Is(err, db.ErrUnsupported), true)
	})
	t.Run("update with join and limit go through rowid", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Table("users as u").Join("posts as p", "p.user_id", "u.id").Where("p.views", ">", 6).Update(ctx, map[string]any{"u.active": 0})
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("posts").Where("views", ">", 0).OrderBy("views", "desc").Limit(1).Update(ctx, map[string]any{"title": "top"})
		noErr(t, err)
		eq(t, n, int64(1))
		var title string
		noErr(t, conn.Table("posts").Where("views", 10).Value(ctx, "title", &title))
		eq(t, title, "top")
	})
	t.Run("delete variants", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Table("posts").Where("published", 0).Delete(ctx)
		noErr(t, err)
		eq(t, n, int64(1))
		n, err = conn.Table("posts").Where("views", ">", 0).OrderBy("views").Limit(1).Delete(ctx)
		noErr(t, err)
		eq(t, n, int64(1))
		n, err = conn.Table("posts as p").Join("users as u", "u.id", "p.user_id").Where("u.name", "Bob").Delete(ctx)
		noErr(t, err)
		eq(t, n, int64(1))
		_, err = conn.Table("posts").Delete(ctx)
		eq(t, err, db.ErrNoWhere)
		n, err = conn.Table("posts").AllRows().Delete(ctx)
		noErr(t, err)
		eq(t, n, int64(1))
	})
	t.Run("truncate resets autoincrement", func(t *testing.T) {
		conn := newDB(t)
		noErr(t, conn.Table("posts").Truncate(ctx))
		c, _ := conn.Table("posts").Count(ctx)
		eq(t, c, int64(0))
		id, err := conn.Table("posts").InsertGetID(ctx, map[string]any{"title": "fresh"})
		noErr(t, err)
		eq(t, id, int64(1))
	})
	t.Run("returning", func(t *testing.T) {
		conn := newDB(t)
		var out []struct {
			ID   int64
			Name string
		}
		noErr(t, conn.Table("users").Returning("id", "name").InsertReturning(ctx, &out, map[string]any{"name": "Rae", "email": "rae@x"}))
		eq(t, len(out), 1)
		eq(t, out[0].Name, "Rae")
		noErr(t, conn.Table("users").Where("name", "Rae").Returning("id", "name").UpdateReturning(ctx, &out, map[string]any{"name": "Rae2"}))
		eq(t, out[0].Name, "Rae2")
		noErr(t, conn.Table("users").Where("name", "Rae2").Returning("id", "name").DeleteReturning(ctx, &out))
		eq(t, len(out), 1)
		if err := conn.Table("users").Where("id", 1).DeleteReturning(ctx, &out); !errors.Is(err, db.ErrInvalidArgs) {
			t.Errorf("returning without columns: %v", err)
		}
	})
	t.Run("not bound", func(t *testing.T) {
		_, err := db.Use(db.SQLite).Table("users").Insert(ctx, map[string]any{"a": 1})
		eq(t, err, db.ErrNotBound)
		eq(t, db.Use(db.SQLite).Table("users").Truncate(ctx), db.ErrNotBound)
	})
}

func TestSQLitePagination(t *testing.T) {
	conn := newDB(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	t.Run("length aware", func(t *testing.T) {
		var us []User
		p, err := users().Paginate(ctx, 2, 3, &us)
		noErr(t, err)
		eq(t, p, db.Page{Total: 4, PerPage: 3, CurrentPage: 2, LastPage: 2, From: 4, To: 4})
		eq(t, names(us), []string{"Di"})
		p, err = users().Paginate(ctx, 1, 3, &us)
		noErr(t, err)
		eq(t, p, db.Page{Total: 4, PerPage: 3, CurrentPage: 1, LastPage: 2, From: 1, To: 3})
		p, err = users().Where("id", 999).Paginate(ctx, 1, 3, &us)
		noErr(t, err)
		eq(t, p, db.Page{Total: 0, PerPage: 3, CurrentPage: 1, LastPage: 1})
		eq(t, len(us), 0)
		_, err = users().Paginate(ctx, 1, 0, &us)
		eq(t, errors.Is(err, db.ErrInvalidArgs), true)
	})
	t.Run("simple", func(t *testing.T) {
		var us []User
		p, err := users().SimplePaginate(ctx, 1, 3, &us)
		noErr(t, err)
		eq(t, p, db.SimplePage{PerPage: 3, CurrentPage: 1, HasMore: true})
		eq(t, names(us), []string{"Ann", "Bob", "Cy"})
		p, err = users().SimplePaginate(ctx, 2, 3, &us)
		noErr(t, err)
		eq(t, p.HasMore, false)
		eq(t, names(us), []string{"Di"})
	})
	t.Run("cursor walks every row once", func(t *testing.T) {
		var seen []string
		cursor := ""
		for range 5 {
			var us []User
			p, err := conn.Table("users").CursorPaginate(ctx, 3, cursor, &us)
			noErr(t, err)
			seen = append(seen, names(us)...)
			if !p.HasMore {
				break
			}
			cursor = p.NextCursor
		}
		eq(t, seen, []string{"Ann", "Bob", "Cy", "Di"})
	})
	t.Run("cursor descending on maps", func(t *testing.T) {
		var rows []map[string]any
		p, err := conn.Table("users").CursorPaginate(ctx, 2, "", &rows, db.CursorOptions{Column: "id", Desc: true})
		noErr(t, err)
		eq(t, rows[0]["id"], int64(4))
		eq(t, p.HasMore, true)
		p, err = conn.Table("users").CursorPaginate(ctx, 2, p.NextCursor, &rows, db.CursorOptions{Column: "id", Desc: true})
		noErr(t, err)
		eq(t, rows[0]["id"], int64(2))
		eq(t, p.HasMore, false)
	})
	t.Run("malformed cursor", func(t *testing.T) {
		var us []User
		for _, c := range []string{"!!!", "bm90LWpzb24"} {
			if _, err := conn.Table("users").CursorPaginate(ctx, 2, c, &us); !errors.Is(err, db.ErrInvalidArgs) {
				t.Errorf("cursor %q: %v", c, err)
			}
		}
	})
}

func TestSQLiteChunkingAndStreaming(t *testing.T) {
	conn := newDB(t)

	t.Run("chunk needs order and walks pages", func(t *testing.T) {
		var us []User
		eq(t, conn.Table("users").Chunk(ctx, 2, &us, func() (bool, error) { return true, nil }), db.ErrNoOrder)
		var pages [][]string
		noErr(t, conn.Table("users").OrderBy("id").Chunk(ctx, 3, &us, func() (bool, error) {
			pages = append(pages, names(us))
			return true, nil
		}))
		eq(t, pages, [][]string{{"Ann", "Bob", "Cy"}, {"Di"}})
	})
	t.Run("chunk stops early and propagates errors", func(t *testing.T) {
		calls := 0
		var us []User
		noErr(t, conn.Table("users").OrderBy("id").Chunk(ctx, 1, &us, func() (bool, error) { calls++; return calls < 2, nil }))
		eq(t, calls, 2)
		boom := errors.New("boom")
		eq(t, conn.Table("users").OrderBy("id").Chunk(ctx, 1, &us, func() (bool, error) { return true, boom }), boom)
	})
	t.Run("chunk by id survives deletes made by the callback", func(t *testing.T) {
		var total int
		noErr(t, db.As[map[string]any](conn.Table("posts")).ChunkByID(ctx, 2, func(rows []map[string]any) (bool, error) {
			total += len(rows)
			for _, r := range rows {
				if _, err := conn.Table("posts").Where("id", r["id"]).Delete(ctx); err != nil {
					return false, err
				}
			}
			return true, nil
		}))
		eq(t, total, 4)
		c, _ := conn.Table("posts").Count(ctx)
		eq(t, c, int64(0))
	})
	t.Run("typed chunk", func(t *testing.T) {
		var got []string
		noErr(t, db.As[User](conn.Table("users").OrderBy("id")).Chunk(ctx, 3, func(us []User) (bool, error) {
			got = append(got, names(us)...)
			return true, nil
		}))
		eq(t, got, []string{"Ann", "Bob", "Cy", "Di"})
	})
	t.Run("each", func(t *testing.T) {
		var u User
		var got []string
		noErr(t, conn.Table("users").OrderBy("id").Each(ctx, &u, func() error { got = append(got, u.Name); return nil }))
		eq(t, got, []string{"Ann", "Bob", "Cy", "Di"})
		stop := errors.New("stop")
		eq(t, conn.Table("users").Each(ctx, &u, func() error { return stop }), stop)
		eq(t, conn.Table("users").Each(ctx, u, func() error { return nil }) != nil, true) // non-pointer
	})
	t.Run("stream", func(t *testing.T) {
		var got []string
		for u, err := range db.Stream[User](ctx, conn.Table("users").OrderBy("id")) {
			noErr(t, err)
			got = append(got, u.Name)
			if u.Name == "Bob" {
				break // early exit must close the cursor
			}
		}
		eq(t, got, []string{"Ann", "Bob"})
		c, err := conn.Table("users").Count(ctx) // single connection: fails if the cursor leaked
		noErr(t, err)
		eq(t, c, int64(4))

		var gotErr error
		for _, err := range db.Stream[User](ctx, conn.Table("nope")) {
			gotErr = err
		}
		if gotErr == nil {
			t.Error("stream over a missing table should yield an error")
		}
		for m, err := range db.Stream[map[string]any](ctx, conn.Table("users").Select("name").Where("id", 1)) {
			noErr(t, err)
			eq(t, m["name"], "Ann")
		}
	})
}

func TestSQLiteTransactions(t *testing.T) {
	count := func(conn *db.DB) int64 {
		n, err := conn.Table("users").Count(ctx)
		noErr(t, err)
		return n
	}
	t.Run("commit", func(t *testing.T) {
		conn := newDB(t)
		noErr(t, conn.Transaction(ctx, func(tx *db.DB) error {
			_, err := tx.Table("users").Insert(ctx, map[string]any{"name": "T", "email": "t@x"})
			return err
		}))
		eq(t, count(conn), int64(5))
	})
	t.Run("rollback on error", func(t *testing.T) {
		conn := newDB(t)
		boom := errors.New("boom")
		err := conn.Transaction(ctx, func(tx *db.DB) error {
			if _, err := tx.Table("users").Insert(ctx, map[string]any{"name": "T", "email": "t@x"}); err != nil {
				return err
			}
			return boom
		})
		eq(t, err, boom)
		eq(t, count(conn), int64(4))
	})
	t.Run("rollback on panic and re-panic", func(t *testing.T) {
		conn := newDB(t)
		func() {
			defer func() { eq(t, recover(), any("kaboom")) }()
			_ = conn.Transaction(ctx, func(tx *db.DB) error {
				_, _ = tx.Table("users").Insert(ctx, map[string]any{"name": "T", "email": "t@x"})
				panic("kaboom")
			})
			t.Error("panic should propagate")
		}()
		eq(t, count(conn), int64(4)) // rolled back, and the connection is usable again
	})
	t.Run("nested savepoints roll back independently", func(t *testing.T) {
		conn := newDB(t)
		boom := errors.New("inner")
		noErr(t, conn.Transaction(ctx, func(tx *db.DB) error {
			if _, err := tx.Table("users").Insert(ctx, map[string]any{"name": "Outer", "email": "o@x"}); err != nil {
				return err
			}
			err := tx.Transaction(ctx, func(inner *db.DB) error {
				if _, err := inner.Table("users").Insert(ctx, map[string]any{"name": "Inner", "email": "i@x"}); err != nil {
					return err
				}
				return boom
			})
			eq(t, err, boom)
			// a second savepoint that succeeds
			return tx.Transaction(ctx, func(inner *db.DB) error {
				_, err := inner.Table("users").Insert(ctx, map[string]any{"name": "Inner2", "email": "i2@x"})
				return err
			})
		}))
		var got []string
		noErr(t, conn.Table("users").Where("id", ">", 4).OrderBy("id").Pluck(ctx, "name", &got))
		eq(t, got, []string{"Outer", "Inner2"})
	})
	t.Run("using an existing *sql.Tx", func(t *testing.T) {
		sdb, _ := sql.Open("sqlite", ":memory:")
		sdb.SetMaxOpenConns(1)
		defer sdb.Close()
		_, err := sdb.Exec(schema)
		noErr(t, err)
		stx, err := sdb.Begin()
		noErr(t, err)
		conn := db.New(stx, db.SQLite)
		boom := errors.New("x")
		eq(t, conn.Transaction(ctx, func(tx *db.DB) error {
			_, _ = tx.Table("users").Insert(ctx, map[string]any{"name": "S", "email": "s@x"})
			return boom
		}), boom)
		eq(t, count(conn), int64(4)) // savepoint rolled back
		noErr(t, stx.Rollback())
	})
	t.Run("connection that cannot begin", func(t *testing.T) {
		conn := db.New(noBegin{newDB(t)}, db.SQLite)
		eq(t, conn.Transaction(ctx, func(*db.DB) error { return nil }), db.ErrNoTransaction)
	})
}

// noBegin hides BeginTx so the DB looks like a bare Conn.
type noBegin struct{ conn *db.DB }

func (n noBegin) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	return nil, errors.New("unused")
}
func (n noBegin) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return nil, errors.New("unused")
}

func TestSQLiteHooksAndRawAccess(t *testing.T) {
	t.Run("args are redacted by default", func(t *testing.T) {
		var evs []db.QueryEvent
		conn := newDB(t, db.WithHook(func(_ context.Context, e db.QueryEvent) { evs = append(evs, e) }))
		var us []User
		noErr(t, conn.Table("users").Where("email", "ann@x").Get(ctx, &us))
		eq(t, len(evs), 1)
		eq(t, evs[0].NumArgs, 1)
		eq(t, evs[0].Args == nil, true)
		eq(t, strings.Contains(evs[0].SQL, "ann@x"), false)
		eq(t, evs[0].Duration >= 0, true)
	})
	t.Run("args logged on request, errors reported", func(t *testing.T) {
		var evs []db.QueryEvent
		conn := newDB(t, db.WithLogArgs(), db.WithHook(func(_ context.Context, e db.QueryEvent) { evs = append(evs, e) }))
		_, err := conn.Exec(ctx, "insert into nope values (?)", 1)
		if err == nil {
			t.Fatal("want error")
		}
		eq(t, evs[0].Args, []any{1})
		eq(t, evs[0].Err != nil, true)
	})
	t.Run("raw query and exec", func(t *testing.T) {
		conn := newDB(t)
		n, err := conn.Exec(ctx, "update users set active = ? where id = ?", 0, 1)
		noErr(t, err)
		eq(t, n, int64(1))
		var us []User
		noErr(t, conn.Query(ctx, &us, "select * from users where active = ? order by id", 0))
		eq(t, names(us), []string{"Ann", "Bob"})
	})
	t.Run("prefix option and dialect accessor", func(t *testing.T) {
		sdb, _ := sql.Open("sqlite", ":memory:")
		sdb.SetMaxOpenConns(1)
		defer sdb.Close()
		_, err := sdb.Exec(`create table pre_t (id integer primary key, v text); insert into pre_t(v) values ('x')`)
		noErr(t, err)
		conn := db.New(sdb, db.SQLite, db.WithPrefix("pre_"))
		eq(t, conn.Dialect(), db.SQLite)
		var v string
		noErr(t, conn.Table("t").Where("t.id", 1).Value(ctx, "v", &v))
		eq(t, v, "x")
	})
}

func TestSQLiteTypedQueries(t *testing.T) {
	conn := newDB(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	t.Run("get first find sole", func(t *testing.T) {
		us, err := db.As[User](users()).Get(ctx)
		noErr(t, err)
		eq(t, names(us), []string{"Ann", "Bob", "Cy", "Di"})
		u, err := db.As[User](users().Where("name", "Bob")).First(ctx)
		noErr(t, err)
		eq(t, u.Name, "Bob")
		u, err = db.As[User](conn.Table("users")).Find(ctx, 3)
		noErr(t, err)
		eq(t, u.Name, "Cy")
		u, err = db.As[User](users().Where("name", "Ann")).Sole(ctx)
		noErr(t, err)
		eq(t, u.Name, "Ann")
		_, err = db.As[User](users().Where("age", ">", 0)).Sole(ctx)
		eq(t, err, db.ErrMultipleRecords)
		_, err = db.As[User](users().Where("name", "nobody")).First(ctx)
		eq(t, errors.Is(err, db.ErrNotFound), true)
		// the handle gives the same builder back for further chaining
		b := users()
		eq(t, db.As[User](b).Builder() == b, true)
	})
	t.Run("maps and scalars", func(t *testing.T) {
		m, err := db.As[map[string]any](conn.Table("users").Select("name").Where("id", 1)).First(ctx)
		noErr(t, err)
		eq(t, m["name"], "Ann")
		ids, err := db.As[int64](conn.Table("users").Select("id").OrderBy("id")).Get(ctx)
		noErr(t, err)
		eq(t, ids, []int64{1, 2, 3, 4})

		name, err := db.ValueAs[string](ctx, conn.Table("users").Where("id", 2), "name")
		noErr(t, err)
		eq(t, name, "Bob")
		age, err := db.ValueAs[int](ctx, conn.Table("users").Where("id", 4), "age") // NULL -> zero
		noErr(t, err)
		eq(t, age, 0)
		ns, err := db.PluckAs[string](ctx, users().Where("active", 1), "name")
		noErr(t, err)
		eq(t, ns, []string{"Ann", "Cy", "Di"})
		mn, err := db.MinAs[int](ctx, conn.Table("users"), "age")
		noErr(t, err)
		mx, err := db.MaxAs[int](ctx, conn.Table("users"), "age")
		noErr(t, err)
		eq(t, [2]int{mn, mx}, [2]int{25, 41})
		sum, err := db.AggregateAs[float64](ctx, conn.Table("users"), "sum", "age")
		noErr(t, err)
		eq(t, sum, 96.0)
	})
	t.Run("pagination carries its rows", func(t *testing.T) {
		p, err := db.As[User](users()).Paginate(ctx, 2, 3)
		noErr(t, err)
		eq(t, p.Total, int64(4)) // promoted from the embedded Page
		eq(t, p.LastPage, 2)
		eq(t, names(p.Items), []string{"Di"})
		sp, err := db.As[User](users()).SimplePaginate(ctx, 1, 3)
		noErr(t, err)
		eq(t, sp.HasMore, true)
		eq(t, len(sp.Items), 3)

		var seen []string
		cursor := ""
		for range 5 {
			cp, err := db.As[User](conn.Table("users")).CursorPaginate(ctx, 3, cursor)
			noErr(t, err)
			seen = append(seen, names(cp.Items)...)
			if !cp.HasMore {
				break
			}
			cursor = cp.NextCursor
		}
		eq(t, seen, []string{"Ann", "Bob", "Cy", "Di"})
	})
	t.Run("chunk each stream", func(t *testing.T) {
		var pages []int
		noErr(t, db.As[User](users()).Chunk(ctx, 3, func(us []User) (bool, error) {
			pages = append(pages, len(us))
			return true, nil
		}))
		eq(t, pages, []int{3, 1})
		var got []string
		noErr(t, db.As[User](users()).ChunkByID(ctx, 2, func(us []User) (bool, error) {
			got = append(got, names(us)...)
			return true, nil
		}))
		eq(t, got, []string{"Ann", "Bob", "Cy", "Di"})
		got = nil
		noErr(t, db.As[User](users()).Each(ctx, func(u User) error { got = append(got, u.Name); return nil }))
		eq(t, got, []string{"Ann", "Bob", "Cy", "Di"})
		got = nil
		for u, err := range db.As[User](users()).Stream(ctx) {
			noErr(t, err)
			got = append(got, u.Name)
			if u.Name == "Bob" {
				break
			}
		}
		eq(t, got, []string{"Ann", "Bob"})
	})
	t.Run("returning gives typed rows", func(t *testing.T) {
		rows, err := db.As[Member](conn.Table("users")).InsertReturning(ctx, Member{Name: "Rae", Email: ptr("rae@x")})
		noErr(t, err)
		eq(t, len(rows), 1)
		eq(t, rows[0].Name, "Rae")
		eq(t, rows[0].ID > 4, true)
		rows, err = db.As[Member](conn.Table("users").Where("name", "Rae")).UpdateReturning(ctx, Member{Bio: "hello"})
		noErr(t, err)
		eq(t, rows[0].Bio, "hello")
		eq(t, rows[0].Name, "Rae") // untouched columns survive
		rows, err = db.As[Member](conn.Table("users").Where("name", "Rae")).DeleteReturning(ctx)
		noErr(t, err)
		eq(t, len(rows), 1)
	})
}

func TestSQLiteStructWrites(t *testing.T) {
	t.Run("insert variants", func(t *testing.T) {
		conn := newDB(t)
		n, err := db.As[Member](conn.Table("users")).Insert(ctx, Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")})
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = db.As[Member](conn.Table("users")).Insert(ctx) // nothing to insert
		noErr(t, err)
		eq(t, n, int64(0))
		id, err := db.As[Member](conn.Table("users")).InsertGetID(ctx, Member{Name: "Gus", Email: ptr("gus@x")})
		noErr(t, err)
		eq(t, id, int64(7))
		n, err = db.As[Member](conn.Table("users")).InsertOrIgnore(ctx, Member{Name: "Dup", Email: ptr("ann@x")})
		noErr(t, err)
		eq(t, n, int64(0))
		// pointers to structs work too
		n, err = db.As[*Member](conn.Table("users")).Insert(ctx, &Member{Name: "Hal", Email: ptr("hal@x")})
		noErr(t, err)
		eq(t, n, int64(1))
		// the column default applies because omitted fields are not written
		var active bool
		noErr(t, conn.Table("users").Where("name", "Eve").Value(ctx, "active", &active))
		eq(t, active, true)
	})
	t.Run("upsert", func(t *testing.T) {
		conn := newDB(t)
		_, err := db.As[Member](conn.Table("users")).Upsert(ctx, []Member{
			{Name: "Ann!", Email: ptr("ann@x"), Age: ptr(31)},
			{Name: "New", Email: ptr("new@x"), Age: ptr(1)},
		}, []string{"email"}, []string{"name", "age"})
		noErr(t, err)
		var name string
		noErr(t, conn.Table("users").Where("email", "ann@x").Value(ctx, "name", &name))
		eq(t, name, "Ann!")
		c, _ := conn.Table("users").Count(ctx)
		eq(t, c, int64(5))
	})
	t.Run("update writes only what is set", func(t *testing.T) {
		conn := newDB(t)
		n, err := db.As[Member](conn.Table("users").Where("id", 1)).Update(ctx, Member{Name: "Annie"})
		noErr(t, err)
		eq(t, n, int64(1))
		var u User
		noErr(t, conn.Table("users").Find(ctx, 1, &u))
		eq(t, u.Name, "Annie")
		eq(t, u.Age != nil && *u.Age == 30, true) // not overwritten by a zero value

		// a pointer to zero is "set": it writes 0, where a plain zero would be skipped
		_, err = db.As[Member](conn.Table("users").Where("id", 1)).Update(ctx, Member{Age: ptr(0)})
		noErr(t, err)
		noErr(t, conn.Table("users").Find(ctx, 1, &u))
		eq(t, u.Age != nil && *u.Age == 0, true)

		_, err = db.As[Member](conn.Table("users").Where("id", 1)).Update(ctx, Member{})
		eq(t, err, db.ErrNoValues)
		_, err = db.As[Member](conn.Table("users")).Update(ctx, Member{Name: "x"})
		eq(t, err, db.ErrNoWhere)
	})
	t.Run("bad input is an error", func(t *testing.T) {
		conn := newDB(t)
		_, err := db.As[int](conn.Table("users")).Insert(ctx, 5)
		eq(t, errors.Is(err, db.ErrInvalidArgs), true)
		_, err = db.As[map[string]any](conn.Table("users").Where("id", 1)).Update(ctx, map[string]any{"a": 1})
		eq(t, errors.Is(err, db.ErrInvalidArgs), true)
	})
}

func TestSQLiteFromTakesTheTableFromTheType(t *testing.T) {
	conn := newDB(t)

	t.Run("reads", func(t *testing.T) {
		us, err := db.From[User](conn).Where("active", 1).OrderBy("id").Get(ctx)
		noErr(t, err)
		eq(t, names(us), []string{"Ann", "Cy", "Di"})
		u, err := db.From[User](conn).Find(ctx, 2)
		noErr(t, err)
		eq(t, u.Name, "Bob")
		n, err := db.From[User](conn).Where("age", ">", 26).Count(ctx)
		noErr(t, err)
		eq(t, n, int64(2))
		ok, err := db.From[User](conn).Where("name", "Ann").Exists(ctx)
		noErr(t, err)
		eq(t, ok, true)
		gone, err := db.From[User](conn).Where("name", "zzz").DoesntExist(ctx)
		noErr(t, err)
		eq(t, gone, true)
		sum, err := db.From[Post](conn).Sum(ctx, "views") // pointer-receiver TableName
		noErr(t, err)
		eq(t, sum, 23.0)
		avg, err := db.From[Post](conn).Avg(ctx, "views")
		noErr(t, err)
		eq(t, avg, 5.75)
		p, err := db.From[User](conn).OrderBy("id").Paginate(ctx, 2, 3)
		noErr(t, err)
		eq(t, [2]any{p.Total, names(p.Items)[0]}, [2]any{int64(4), "Di"})
	})
	t.Run("chain helpers and Apply reach any builder method", func(t *testing.T) {
		us, err := db.From[User](conn).
			When(true, func(q *db.Query[User]) { q.Where("active", 1) }).
			Apply(func(b *db.Builder) { b.WhereYear("created_at", 2024) }).
			OrderBy("id").
			Get(ctx)
		noErr(t, err)
		eq(t, names(us), []string{"Ann"})
	})
	t.Run("writes need no table name or builder", func(t *testing.T) {
		n, err := db.From[Member](conn).Insert(ctx, Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")})
		noErr(t, err)
		eq(t, n, int64(2))
		id, err := db.From[Member](conn).InsertGetID(ctx, Member{Name: "Gus", Email: ptr("gus@x")})
		noErr(t, err)
		eq(t, id, int64(7))
		n, err = db.From[Member](conn).InsertOrIgnore(ctx, Member{Name: "Dup", Email: ptr("ann@x")})
		noErr(t, err)
		eq(t, n, int64(0))
		_, err = db.From[Member](conn).Upsert(ctx, []Member{{Name: "Ann!", Email: ptr("ann@x")}}, []string{"email"}, []string{"name"})
		noErr(t, err)

		n, err = db.From[Member](conn).Where("id", 1).Update(ctx, Member{Bio: "set"})
		noErr(t, err)
		eq(t, n, int64(1))
		n, err = db.From[Member](conn).Where("id", 1).UpdateMap(ctx, map[string]any{"bio": nil}) // explicit NULL
		noErr(t, err)
		eq(t, n, int64(1))
		_, err = db.From[Member](conn).Update(ctx, Member{Name: "x"})
		eq(t, err, db.ErrNoWhere)

		n, err = db.From[Member](conn).Where("name", "Fay").Delete(ctx)
		noErr(t, err)
		eq(t, n, int64(1))
		_, err = db.From[Member](conn).Delete(ctx)
		eq(t, err, db.ErrNoWhere)

		rows, err := db.From[Member](conn).InsertReturning(ctx, Member{Name: "Rae", Email: ptr("rae@x")})
		noErr(t, err)
		eq(t, rows[0].Name, "Rae")

		noErr(t, db.From[Post](conn).Truncate(ctx))
		c, err := db.From[Post](conn).Count(ctx)
		noErr(t, err)
		eq(t, c, int64(0))
		pid, err := db.From[Post](conn).InsertGetID(ctx, Post{Title: "fresh"})
		noErr(t, err)
		eq(t, pid, int64(1))
	})
	t.Run("a type without TableName fails clearly", func(t *testing.T) {
		_, err := db.From[Untitled](conn).Get(ctx)
		eq(t, errors.Is(err, db.ErrInvalidArgs), true)
		_, err = db.From[Untitled](conn).Where("id", 1).Count(ctx)
		eq(t, errors.Is(err, db.ErrInvalidArgs), true)
		_, err = db.From[Untitled](conn).Insert(ctx, Untitled{ID: 1})
		eq(t, errors.Is(err, db.ErrInvalidArgs), true)
		// As still works for types without one: the table is given explicitly
		us, err := db.As[Untitled](conn.Table("users").Select("id").OrderBy("id")).Get(ctx)
		noErr(t, err)
		total, err := conn.Table("users").Count(ctx)
		noErr(t, err)
		eq(t, int64(len(us)), total)
	})
}

func TestSQLiteCTE(t *testing.T) {
	conn := newDB(t)
	active := func() *db.Builder { return conn.Table("users").Where("active", true) }

	t.Run("plain", func(t *testing.T) {
		eq(t, pluckNames(t, conn.Table("au").With("au", active()).OrderBy("name")), []string{"Ann", "Cy", "Di"})
	})
	t.Run("recursive series", func(t *testing.T) {
		anchor := conn.Table("users").SelectRaw("1 as n").Where("id", 1)
		step := conn.Table("seq").SelectRaw("n + 1").Where("n", "<", 5)
		var got []int64
		noErr(t, conn.Table("seq").WithRecursive("seq", anchor.UnionAll(step), "n").OrderBy("n").Pluck(ctx, "n", &got))
		eq(t, got, []int64{1, 2, 3, 4, 5})
	})
	t.Run("inside where in", func(t *testing.T) {
		var got []string
		sub := conn.Table("au").Select("id").With("au", active())
		noErr(t, conn.Table("posts").WhereIn("user_id", sub).OrderBy("title").Pluck(ctx, "title", &got))
		eq(t, got, []string{"p1", "p2", "p4"})
	})
	t.Run("with an outer union", func(t *testing.T) {
		q := conn.Table("au").Select("name").With("au", active()).Where("name", "Ann").
			UnionAll(conn.Table("users").Select("name").Where("name", "Bob")).OrderBy("name")
		eq(t, pluckNames(t, q), []string{"Ann", "Bob"})
	})
	t.Run("count and exists", func(t *testing.T) {
		n, err := conn.Table("au").With("au", active()).Count(ctx)
		noErr(t, err)
		eq(t, n, int64(3))
		ok, err := conn.Table("au").With("au", active()).Where("name", "Bob").Exists(ctx)
		noErr(t, err)
		eq(t, ok, false)
	})
	t.Run("insert using", func(t *testing.T) {
		sub := conn.Table("inactive").Select("name").With("inactive", conn.Table("users").Where("active", false))
		n, err := conn.Table("users").InsertUsing(ctx, []string{"name"}, sub)
		noErr(t, err)
		eq(t, n, int64(1))
		c, err := conn.Table("users").Where("name", "Bob").Count(ctx)
		noErr(t, err)
		eq(t, c, int64(2))
	})
}
