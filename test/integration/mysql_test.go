package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"

	"github.com/akfaiz/quill/db"
)

const mysqlImage = "mysql:8.4"

// mysqlServer is the MySQL container shared by every test in this file.
type mysqlServer struct {
	raw  *sql.DB
	conn *db.DB
}

var (
	mysqlMu     sync.Mutex
	mysqlShared *mysqlServer
)

// startMySQL starts the container on first use and skips when Docker is unavailable.
func startMySQL(t *testing.T) *mysqlServer {
	t.Helper()
	requireContainers(t)

	mysqlMu.Lock()
	defer mysqlMu.Unlock()
	if mysqlShared != nil {
		return mysqlShared
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c, err := tcmysql.Run(ctx, mysqlImage,
		tcmysql.WithDatabase("gq"), tcmysql.WithUsername("gq"), tcmysql.WithPassword("gq"))
	if c != nil {
		onShutdown(terminate(c))
	}
	if err != nil {
		t.Fatalf("start mysql: %v", err)
	}
	dsn, err := c.ConnectionString(ctx, "parseTime=true")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	onShutdown(func(context.Context) { _ = raw.Close() }) // runs before the container is removed
	raw.SetMaxOpenConns(8)
	if err := raw.PingContext(ctx); err != nil {
		t.Fatalf("ping mysql: %v", err)
	}
	mysqlShared = &mysqlServer{raw: raw, conn: db.New(raw, db.MySQL)}
	return mysqlShared
}

// reset recreates the schema and seed rows so every test starts from the same state.
func (s *mysqlServer) reset(t *testing.T) *db.DB {
	t.Helper()
	execAll(t, s.raw, append([]string{
		`drop table if exists posts`,
		`drop table if exists users`,
		`create table users (
		   id int auto_increment primary key, name varchar(100) not null, email varchar(100) unique, age int,
		   active tinyint(1) not null default 1, score double not null default 0,
		   bio text, meta json, created_at datetime)`,
		`create table posts (
		   id int auto_increment primary key, user_id int, title varchar(100),
		   views int not null default 0, published tinyint(1) not null default 0)`,
	}, seedStatements...))
	return s.conn
}

func TestMySQLScanningAndReads(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	var us []User
	noErr(t, users().Get(ctx, &us))
	eq(t, names(us), []string{"Ann", "Bob", "Cy", "Di"})
	eq(t, us[2].Bio, "hello")
	eq(t, us[0].Bio, "") // NULL -> zero value
	eq(t, us[3].Age == nil && us[0].Age != nil && *us[0].Age == 30, true)
	eq(t, us[1].Active, false)
	eq(t, us[0].Score, 1.5)

	var rows []map[string]any
	noErr(t, conn.Table("users").Select("name", "age").OrderBy("id").Get(ctx, &rows))
	eq(t, rows[0]["name"], "Ann") // text columns arrive as string, not []byte
	eq(t, rows[3]["age"], nil)

	var u User
	noErr(t, users().Where("name", "Bob").Sole(ctx, &u))
	eq(t, users().Where("age", ">", 0).Sole(ctx, &u), db.ErrMultipleRecords)
	eq(t, users().Where("age", 999).First(ctx, &u), db.ErrNotFound)
	noErr(t, users().Find(ctx, 3, &u))
	eq(t, u.Name, "Cy")

	var ids map[int64]string
	noErr(t, users().PluckMap(ctx, "name", "id", &ids))
	eq(t, ids, map[int64]string{1: "Ann", 2: "Bob", 3: "Cy", 4: "Di"})

	ok, err := users().Where("name", "Ann").Exists(ctx)
	noErr(t, err)
	eq(t, ok, true)
	no, err := users().Where("name", "zzz").DoesntExist(ctx)
	noErr(t, err)
	eq(t, no, true)
}

func TestMySQLAggregates(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	n, err := conn.Table("users").Count(ctx)
	noErr(t, err)
	eq(t, n, int64(4))
	n, err = conn.Table("users").Count(ctx, "age")
	noErr(t, err)
	eq(t, n, int64(3))
	sum, err := conn.Table("users").Sum(ctx, "age")
	noErr(t, err)
	eq(t, sum, 96.0)
	avg, err := conn.Table("users").Avg(ctx, "age")
	noErr(t, err)
	eq(t, avg, 32.0)
	var mn, mx int
	noErr(t, conn.Table("users").Min(ctx, "age", &mn))
	noErr(t, conn.Table("users").Max(ctx, "age", &mx))
	eq(t, [2]int{mn, mx}, [2]int{25, 41})
	none, err := conn.Table("users").Where("id", 99).Sum(ctx, "age")
	noErr(t, err)
	eq(t, none, 0.0)

	// wrapped forms: grouped, distinct, limited, joined. Strict servers need the group column selected.
	n, err = conn.Table("posts").Select("user_id").GroupBy("user_id").Count(ctx)
	noErr(t, err)
	eq(t, n, int64(3))
	n, err = conn.Table("posts").Select("user_id").Distinct().Count(ctx)
	noErr(t, err)
	eq(t, n, int64(3))
	n, err = conn.Table("users").OrderBy("id").Limit(2).Count(ctx)
	noErr(t, err)
	eq(t, n, int64(2))
	n, err = conn.Table("posts").Join("users", "users.id", "posts.user_id").Where("posts.views", ">", 0).Count(ctx)
	noErr(t, err)
	eq(t, n, int64(4))
	total, err := conn.Table("posts").OrderBy("id").Limit(3).Sum(ctx, "posts.views")
	noErr(t, err)
	eq(t, total, 22.0)
}

func TestMySQLWhereFamily(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("users.id") }
	posts := func() *db.Builder { return conn.Table("posts") }

	cases := []struct {
		name string
		q    *db.Builder
		want []string
	}{
		{"in / not in", users().WhereIn("id", []int{1, 3}).WhereNotIn("name", []string{"Cy"}), []string{"Ann"}},
		{"empty in", users().WhereIn("id", []int{}), nil},
		{"integer in raw", users().WhereIntegerInRaw("id", []int{2, 4}), []string{"Bob", "Di"}},
		{"between", users().WhereBetween("age", 26, 45), []string{"Ann", "Cy"}},
		{"null", users().WhereNull("age").OrWhereNull("bio"), []string{"Ann", "Bob", "Di"}},
		{"like insensitive", users().WhereLike("name", "a%"), []string{"Ann"}},
		{"like case sensitive miss", users().WhereLike("name", "a%", true), nil},
		{"like case sensitive hit", users().WhereLike("name", "A%", true), []string{"Ann"}},
		{"not like", users().WhereNotLike("name", "%n"), []string{"Bob", "Cy", "Di"}},
		{"basic like operator", users().Where("name", "like", "C%"), []string{"Cy"}},
		{"null safe equals", users().WhereNullSafeEquals("age", nil), []string{"Di"}},
		{"group", users().Where("active", true).WhereGroup(func(q *db.Builder) { q.Where("age", ">", 35).OrWhereNull("age") }), []string{"Cy", "Di"}},
		{"not", users().WhereNot("active", true), []string{"Bob"}},
		{"exists", users().WhereExists(posts().SelectRaw("1").WhereColumn("posts.user_id", "users.id").Where("views", ">", 6)), []string{"Ann", "Bob"}},
		{"not exists", users().WhereNotExists(posts().SelectRaw("1").WhereColumn("posts.user_id", "users.id")), []string{"Di"}},
		{"in subquery", users().WhereIn("id", posts().Select("user_id").Where("published", true)), []string{"Ann", "Bob", "Cy"}},
		{"scalar subquery", users().Where("age", ">", conn.Table("users").SelectRaw("avg(age)")), []string{"Cy"}},
		{"any", users().WhereAny([]string{"name", "email"}, "like", "b%"), []string{"Bob"}},
		{"all", users().WhereAll([]string{"name", "email"}, "like", "%i%"), []string{"Di"}},
		{"none", users().WhereNone([]string{"name", "email"}, "like", "%o%"), []string{"Ann", "Cy", "Di"}},
		{"year month day", users().WhereYear("created_at", 2024).WhereMonth("created_at", 3).WhereDay("created_at", ">", 7), []string{"Bob"}},
		{"date and time", users().WhereDate("created_at", "2024-03-07").WhereTime("created_at", ">=", "09:00:00"), []string{"Ann"}},
		{"date from time.Time", users().WhereDate("created_at", time.Date(2024, 3, 8, 23, 0, 0, 0, time.UTC)), []string{"Bob"}},
		{"row values", users().WhereRowValues([]string{"id", "age"}, "=", []any{1, 30}), []string{"Ann"}},
		{"raw", users().WhereRaw("length(name) = ?", 2), []string{"Cy", "Di"}},
		{"map", users().WhereMap(map[string]any{"active": true, "name": "Cy"}), []string{"Cy"}},
		{"latest limit offset", conn.Table("users").Latest("id").Limit(2).Offset(1), []string{"Cy", "Bob"}},
		{"left join with value", users().LeftJoinFunc("posts", func(j *db.JoinClause) { j.On("posts.user_id", "users.id").Where("posts.views", ">", 8) }).WhereNull("posts.id"), []string{"Bob", "Cy", "Di"}},
		{"join sub", users().JoinSub(posts().Select("user_id").SelectRaw("max(views) as mv").GroupBy("user_id"), "m", "m.user_id", "users.id").Where("m.mv", ">", 6), []string{"Ann", "Bob"}},
		{"cross join", conn.Table("users").CrossJoin("posts").Where("users.id", 1).Where("posts.id", 1), []string{"Ann"}},
		{"from sub", conn.NewQuery().FromSub(conn.Table("users").Where("active", true), "au").OrderBy("name"), []string{"Ann", "Cy", "Di"}},
		{"union", conn.Table("users").Select("name").Where("id", 1).Union(conn.Table("users").Select("name").Where("id", 2)).OrderBy("name", "desc"), []string{"Bob", "Ann"}},
		{"union all limit", conn.Table("users").Select("name").Where("id", 1).UnionAll(conn.Table("users").Select("name").Where("id", 1)).Limit(5), []string{"Ann", "Ann"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pluckNames(t, tc.q)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			eq(t, got, tc.want)
		})
	}

	// valid-SQL-only checks
	for name, q := range map[string]*db.Builder{
		"column compare": posts().WhereColumn("views", ">", "user_id").OrderBy("id"),
		"between cols":   users().WhereBetweenColumns("age", "id", "score"),
		"random order":   users().InRandomOrder().Limit(1),
		"group having":   posts().Select("user_id").SelectRaw("sum(views) as v").GroupBy("user_id").HavingRaw("sum(views) > ?", 6),
	} {
		t.Run("executes: "+name, func(t *testing.T) {
			var rows []map[string]any
			noErr(t, q.Get(ctx, &rows))
		})
	}

	t.Run("select sub value", func(t *testing.T) {
		var rows []map[string]any
		noErr(t, conn.Table("users").Select("name").
			SelectSub(posts().SelectRaw("count(*)").WhereColumn("posts.user_id", "users.id"), "pc").
			OrderBy("id").Get(ctx, &rows))
		eq(t, fmt.Sprint(rows[0]["pc"]), "2")
		eq(t, fmt.Sprint(rows[3]["pc"]), "0")
	})
}

func TestMySQLDialectSpecificJoinsAndLocks(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)

	t.Run("lateral join", func(t *testing.T) {
		latest := conn.Table("posts").Select("title").WhereColumn("posts.user_id", "users.id").OrderBy("views", "desc").Limit(1)
		got := pluckNames(t, conn.Table("users").JoinLateral(latest, "lp").OrderBy("users.id"))
		eq(t, got, []string{"Ann", "Bob", "Cy"})
		got = pluckNames(t, conn.Table("users").LeftJoinLateral(latest, "lp").OrderBy("users.id"))
		eq(t, got, []string{"Ann", "Bob", "Cy", "Di"})
	})
	t.Run("row locks run inside a transaction", func(t *testing.T) {
		noErr(t, conn.Transaction(ctx, func(tx *db.DB) error {
			var u User
			if err := tx.Table("users").Where("id", 1).LockForUpdate().First(ctx, &u); err != nil {
				return err
			}
			return tx.Table("users").Where("id", 2).SharedLock().First(ctx, &u)
		}))
	})
	t.Run("custom lock clause", func(t *testing.T) {
		clause := "for update"
		noErr(t, conn.Transaction(ctx, func(tx *db.DB) error {
			var u User
			return tx.Table("users").Where("id", 1).Lock(clause).First(ctx, &u)
		}))
	})
	t.Run("straight join and index hints", func(t *testing.T) {
		got := pluckNames(t, conn.Table("users").StraightJoin("posts", "posts.user_id", "users.id").Where("posts.id", 1))
		eq(t, got, []string{"Ann"})
		for _, q := range []*db.Builder{
			conn.Table("users").UseIndex("PRIMARY"),
			conn.Table("users").ForceIndex("PRIMARY"),
			conn.Table("users").IgnoreIndex("email"),
		} {
			var rows []map[string]any
			noErr(t, q.Where("id", 1).Get(ctx, &rows))
			eq(t, len(rows), 1)
		}
	})

}

func TestMySQLJSON(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	ordered := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	eq(t, pluckNames(t, ordered().WhereJsonContains("meta->tags", "b")), []string{"Ann", "Bob"})
	eq(t, pluckNames(t, ordered().WhereJsonContains("meta->tags", "a")), []string{"Ann"})
	eq(t, pluckNames(t, ordered().WhereJsonContains("meta", map[string]any{"theme": "light"})), []string{"Bob"})
	// a missing path makes json_contains / @> NULL on Postgres and MySQL, so "doesn't contain" skips Cy
	// (SQLite's json_each form returns true there); same as Laravel.
	eq(t, pluckNames(t, ordered().WhereJsonDoesntContain("meta->tags", "a").WhereNotNull("meta")), []string{"Bob"})
	eq(t, pluckNames(t, ordered().WhereJsonContainsKey("meta->theme")), []string{"Ann", "Bob"})
	eq(t, pluckNames(t, ordered().WhereJsonDoesntContainKey("meta->theme").WhereNotNull("meta")), []string{"Cy"})
	eq(t, pluckNames(t, ordered().WhereJsonLength("meta->tags", ">", 1)), []string{"Ann"})
	eq(t, pluckNames(t, ordered().Where("meta->theme", "light")), []string{"Bob"})

	var theme string
	noErr(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->theme", &theme))
	eq(t, theme, "dark")

	// arrays and objects come back as JSON text, not NULL (MariaDB's json_value would drop them)
	var tags, flags string
	noErr(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->tags", &tags))
	eq(t, tags, `["a", "b"]`)
	noErr(t, conn.Table("users").Where("id", 1).Value(ctx, "meta", &flags))
	eq(t, strings.Contains(flags, `"theme": "dark"`), true)

	eq(t, pluckNames(t, ordered().WhereJsonOverlaps("meta->tags", []string{"a", "z"})), []string{"Ann"})
	eq(t, pluckNames(t, ordered().WhereJsonDoesntOverlap("meta->tags", []string{"a", "z"}).WhereNotNull("meta").WhereJsonContainsKey("meta->tags")), []string{"Bob"})

	t.Run("updates set paths and keep siblings", func(t *testing.T) {
		n, err := conn.Table("users").Where("id", 1).Update(ctx, map[string]any{"meta->theme": "blue", "meta->beta": true})
		noErr(t, err)
		eq(t, n, int64(1))
		noErr(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->theme", &theme))
		eq(t, theme, "blue")
		eq(t, pluckNames(t, ordered().Where("meta->beta", true)), []string{"Ann"})
		eq(t, pluckNames(t, ordered().WhereJsonContains("meta->tags", "a")), []string{"Ann"})
	})
	t.Run("container value", func(t *testing.T) {
		_, err := conn.Table("users").Where("id", 2).Update(ctx, map[string]any{"meta->tags": []string{"x", "y"}})
		noErr(t, err)
		eq(t, pluckNames(t, ordered().WhereJsonContains("meta->tags", "y")), []string{"Bob"})
	})
}

func TestMySQLWrites(t *testing.T) {
	s := startMySQL(t)
	t.Run("insert variants", func(t *testing.T) {
		conn := s.reset(t)
		n, err := conn.Table("users").Insert(ctx, map[string]any{"name": "Eve", "email": "eve@x"}, map[string]any{"name": "Fay", "email": "fay@x"})
		noErr(t, err)
		eq(t, n, int64(2))
		id, err := conn.Table("users").InsertGetID(ctx, map[string]any{"name": "Gus", "email": "gus@x"})
		noErr(t, err)
		eq(t, id, int64(7))
		n, err = conn.Table("users").InsertOrIgnore(ctx, map[string]any{"name": "Dup", "email": "ann@x"})
		noErr(t, err)
		eq(t, n, int64(0))
		n, err = conn.Table("posts").InsertUsing(ctx, []string{"user_id", "title"},
			conn.Table("users").Select("id", "name").Where("id", "<=", 2))
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("users").InsertOrIgnoreUsing(ctx, []string{"name", "email"},
			conn.Table("users").Select("name", "email").Where("id", 1))
		noErr(t, err)
		eq(t, n, int64(0))
		_, err = conn.Table("users").Insert(ctx, map[string]any{"name": "Ann2", "email": "ann@x"})
		if err == nil {
			t.Error("unique violation should error")
		}
	})
	t.Run("upsert and update or insert", func(t *testing.T) {
		conn := s.reset(t)
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
		for _, v := range []string{"Zed", "Zed2"} {
			ok, err := conn.Table("users").UpdateOrInsert(ctx, map[string]any{"email": "zed@x"}, map[string]any{"name": v})
			noErr(t, err)
			eq(t, ok, true)
		}
		noErr(t, conn.Table("users").Where("email", "zed@x").Value(ctx, "name", &name))
		eq(t, name, "Zed2")
	})
	t.Run("update increment guard", func(t *testing.T) {
		conn := s.reset(t)
		n, err := conn.Table("posts").Where("user_id", 1).Increment(ctx, "views", 5, map[string]any{"title": "bumped"})
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("posts").Where("id", 1).Decrement(ctx, "views", 3, nil)
		noErr(t, err)
		eq(t, n, int64(1))
		n, err = conn.Table("posts").Where("id", 2).IncrementEach(ctx, map[string]any{"views": 1, "user_id": 1}, nil)
		noErr(t, err)
		eq(t, n, int64(1))
		var v int
		noErr(t, conn.Table("posts").Where("id", 1).Value(ctx, "views", &v))
		eq(t, v, 12) // 10 + 5 - 3
		_, err = conn.Table("users").Update(ctx, map[string]any{"bio": "x"})
		eq(t, err, db.ErrNoWhere)
		n, err = conn.Table("users").AllRows().Update(ctx, map[string]any{"bio": "all"})
		noErr(t, err)
		eq(t, n, int64(4))
	})
	t.Run("update and delete with join or limit", func(t *testing.T) {
		conn := s.reset(t)
		n, err := conn.Table("users as u").Join("posts as p", "p.user_id", "u.id").Where("p.views", ">", 6).Update(ctx, map[string]any{"u.score": 9}) // MySQL counts changed rows, so pick a column that changes
		noErr(t, err)
		eq(t, n, int64(2))
		n, err = conn.Table("posts").Where("views", ">", 0).OrderBy("views", "desc").Limit(1).Update(ctx, map[string]any{"title": "top"})
		noErr(t, err)
		eq(t, n, int64(1))
		var title string
		noErr(t, conn.Table("posts").Where("views", 10).Value(ctx, "title", &title))
		eq(t, title, "top")

		n, err = conn.Table("posts").Where("published", false).Delete(ctx)
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
	t.Run("truncate resets identity", func(t *testing.T) {
		conn := s.reset(t)
		noErr(t, conn.Table("posts").Truncate(ctx))
		c, _ := conn.Table("posts").Count(ctx)
		eq(t, c, int64(0))
		id, err := conn.Table("posts").InsertGetID(ctx, map[string]any{"title": "fresh"})
		noErr(t, err)
		eq(t, id, int64(1))
	})
	t.Run("returning", func(t *testing.T) {
		conn := s.reset(t)
		var out []struct {
			ID   int64
			Name string
		}
		err := conn.Table("users").Returning("id", "name").InsertReturning(ctx, &out, map[string]any{"name": "Rae", "email": "rae@x"})
		eq(t, errors.Is(err, db.ErrUnsupported), true)
	})
	t.Run("update from", func(t *testing.T) {
		conn := s.reset(t)
		q := conn.Table("users").Join("posts", "posts.user_id", "users.id").Where("posts.views", ">", 9)
		_, err := q.UpdateFrom(ctx, map[string]any{"bio": db.Raw("`posts`.`title`")})
		eq(t, errors.Is(err, db.ErrUnsupported), true)
	})
}

func TestMySQLPaginationAndChunking(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	var us []User
	p, err := users().Paginate(ctx, 2, 3, &us)
	noErr(t, err)
	eq(t, p, db.Page{Total: 4, PerPage: 3, CurrentPage: 2, LastPage: 2, From: 4, To: 4})
	eq(t, names(us), []string{"Di"})
	sp, err := users().SimplePaginate(ctx, 1, 3, &us)
	noErr(t, err)
	eq(t, sp, db.SimplePage{PerPage: 3, CurrentPage: 1, HasMore: true})

	var seen []string
	cursor := ""
	for range 5 {
		var page []User
		cp, err := conn.Table("users").CursorPaginate(ctx, 3, cursor, &page)
		noErr(t, err)
		seen = append(seen, names(page)...)
		if !cp.HasMore {
			break
		}
		cursor = cp.NextCursor
	}
	eq(t, seen, []string{"Ann", "Bob", "Cy", "Di"})

	var pages [][]string
	noErr(t, users().Chunk(ctx, 3, &us, func() (bool, error) { pages = append(pages, names(us)); return true, nil }))
	eq(t, pages, [][]string{{"Ann", "Bob", "Cy"}, {"Di"}})

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

	var got []string
	for u, err := range db.Stream[User](ctx, users()) {
		noErr(t, err)
		got = append(got, u.Name)
		if u.Name == "Bob" {
			break
		}
	}
	eq(t, got, []string{"Ann", "Bob"})
	c, err := conn.Table("users").Count(ctx) // pool still healthy after early exit
	noErr(t, err)
	eq(t, c, int64(4))
}

func TestMySQLTransactions(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	count := func() int64 {
		n, err := conn.Table("users").Count(ctx)
		noErr(t, err)
		return n
	}
	insert := func(tx *db.DB, name string) error {
		_, err := tx.Table("users").Insert(ctx, map[string]any{"name": name, "email": name + "@x"})
		return err
	}

	noErr(t, conn.Transaction(ctx, func(tx *db.DB) error { return insert(tx, "c1") }))
	eq(t, count(), int64(5))

	boom := errors.New("boom")
	eq(t, conn.Transaction(ctx, func(tx *db.DB) error {
		if err := insert(tx, "r1"); err != nil {
			return err
		}
		return boom
	}), boom)
	eq(t, count(), int64(5))

	func() {
		defer func() { eq(t, recover(), any("kaboom")) }()
		_ = conn.Transaction(ctx, func(tx *db.DB) error {
			_ = insert(tx, "p1")
			panic("kaboom")
		})
	}()
	eq(t, count(), int64(5))

	noErr(t, conn.Transaction(ctx, func(tx *db.DB) error {
		if err := insert(tx, "outer"); err != nil {
			return err
		}
		// a real SQL error inside a savepoint must not poison the outer transaction
		err := tx.Transaction(ctx, func(inner *db.DB) error {
			return insert(inner, "outer") // duplicate email
		})
		if err == nil {
			t.Error("duplicate insert should fail")
		}
		return tx.Transaction(ctx, func(inner *db.DB) error { return insert(inner, "inner2") })
	}))
	var got []string
	noErr(t, conn.Table("users").Where("name", "like", "%er%").OrderBy("id").Pluck(ctx, "name", &got))
	eq(t, got, []string{"outer", "inner2"})
	eq(t, count(), int64(7))

	opts := &sql.TxOptions{ReadOnly: true}
	err := conn.TransactionOpts(ctx, opts, func(tx *db.DB) error { return insert(tx, "ro") })
	if err == nil {
		t.Error("write in a read-only transaction should fail")
	}
	eq(t, count(), int64(7))
}

func TestMySQLTypedReadsAndStructWrites(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	us, err := db.As[User](users()).Get(ctx)
	noErr(t, err)
	eq(t, names(us), []string{"Ann", "Bob", "Cy", "Di"})
	u, err := db.As[User](conn.Table("users")).Find(ctx, 3)
	noErr(t, err)
	eq(t, u.Name, "Cy")
	_, err = db.As[User](users().Where("name", "nobody")).First(ctx)
	eq(t, errors.Is(err, db.ErrNotFound), true)

	ids, err := db.As[int64](conn.Table("users").Select("id").OrderBy("id")).Get(ctx)
	noErr(t, err)
	eq(t, ids, []int64{1, 2, 3, 4})
	mx, err := db.MaxAs[int](ctx, conn.Table("users"), "age")
	noErr(t, err)
	eq(t, mx, 41)

	p, err := db.As[User](users()).Paginate(ctx, 2, 3)
	noErr(t, err)
	eq(t, [3]any{p.Total, p.LastPage, names(p.Items)[0]}, [3]any{int64(4), 2, "Di"})
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

	// struct writes
	n, err := db.As[Member](conn.Table("users")).Insert(ctx, Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")})
	noErr(t, err)
	eq(t, n, int64(2))
	id, err := db.As[Member](conn.Table("users")).InsertGetID(ctx, Member{Name: "Gus", Email: ptr("gus@x")})
	noErr(t, err)
	eq(t, id, int64(7))
	n, err = db.As[Member](conn.Table("users")).InsertOrIgnore(ctx, Member{Name: "Dup", Email: ptr("ann@x")})
	noErr(t, err)
	eq(t, n, int64(0))
	_, err = db.As[Member](conn.Table("users")).Upsert(ctx, []Member{{Name: "Ann!", Email: ptr("ann@x"), Age: ptr(31)}},
		[]string{"email"}, []string{"name", "age"})
	noErr(t, err)
	_, err = db.As[Member](conn.Table("users").Where("id", 2)).Update(ctx, Member{Bio: "set"})
	noErr(t, err)
	var rows []User
	noErr(t, conn.Table("users").WhereIn("id", []int{1, 2}).OrderBy("id").Get(ctx, &rows))
	eq(t, [3]any{rows[0].Name, rows[1].Bio, rows[1].Name}, [3]any{"Ann!", "set", "Bob"}) // Bob's name kept

	// MySQL has no RETURNING
	_, err = db.As[Member](conn.Table("users")).InsertReturning(ctx, Member{Name: "Rae", Email: ptr("rae@x")})
	eq(t, errors.Is(err, db.ErrUnsupported), true)
}

func TestMySQLFromTakesTheTableFromTheType(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)

	us, err := db.From[User](conn).Where("active", true).OrderBy("id").Get(ctx)
	noErr(t, err)
	eq(t, names(us), []string{"Ann", "Cy", "Di"})
	u, err := db.From[User](conn).Find(ctx, 2)
	noErr(t, err)
	eq(t, u.Name, "Bob")
	n, err := db.From[User](conn).Where("age", ">", 26).Count(ctx)
	noErr(t, err)
	eq(t, n, int64(2))
	sum, err := db.From[Post](conn).Sum(ctx, "views")
	noErr(t, err)
	eq(t, sum, 23.0)
	p, err := db.From[User](conn).OrderBy("id").Paginate(ctx, 2, 3)
	noErr(t, err)
	eq(t, [2]any{p.Total, names(p.Items)[0]}, [2]any{int64(4), "Di"})

	cnt, err := db.From[Member](conn).Insert(ctx, Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")})
	noErr(t, err)
	eq(t, cnt, int64(2))
	id, err := db.From[Member](conn).InsertGetID(ctx, Member{Name: "Gus", Email: ptr("gus@x")})
	noErr(t, err)
	eq(t, id, int64(7))
	cnt, err = db.From[Member](conn).Where("id", 1).Update(ctx, Member{Bio: "set"})
	noErr(t, err)
	eq(t, cnt, int64(1))
	_, err = db.From[Member](conn).Update(ctx, Member{Name: "x"})
	eq(t, err, db.ErrNoWhere)
	cnt, err = db.From[Member](conn).Where("name", "Fay").Delete(ctx)
	noErr(t, err)
	eq(t, cnt, int64(1))

	noErr(t, db.From[Post](conn).Truncate(ctx))
	pid, err := db.From[Post](conn).InsertGetID(ctx, Post{Title: "fresh"})
	noErr(t, err)
	eq(t, pid, int64(1))

	_, err = db.From[Untitled](conn).Get(ctx)
	eq(t, errors.Is(err, db.ErrInvalidArgs), true)
}

func TestMySQLCTE(t *testing.T) {
	conn := startMySQL(t).reset(t)
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
