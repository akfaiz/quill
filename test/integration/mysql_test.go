package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	startCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c, err := tcmysql.Run(startCtx, mysqlImage,
		tcmysql.WithDatabase("gq"), tcmysql.WithUsername("gq"), tcmysql.WithPassword("gq"))
	if c != nil {
		onShutdown(terminate(c))
	}
	if err != nil {
		t.Fatalf("start mysql: %v", err)
	}
	dsn, err := c.ConnectionString(startCtx, "parseTime=true")
	require.NoError(t, err)
	raw, err := sql.Open("mysql", dsn)
	require.NoError(t, err)
	onShutdown(func(context.Context) { _ = raw.Close() }) // runs before the container is removed
	raw.SetMaxOpenConns(8)
	if err := raw.PingContext(startCtx); err != nil {
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
	require.NoError(t, users().Get(ctx, &us))
	assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, names(us))
	assert.Equal(t, "hello", us[2].Bio)
	assert.Empty(t, us[0].Bio) // NULL -> zero value
	assert.True(t, us[3].Age == nil && us[0].Age != nil && *us[0].Age == 30)
	assert.False(t, us[1].Active)
	assert.Equal(t, 1.5, us[0].Score)

	var rows []map[string]any
	require.NoError(t, conn.Table("users").Select("name", "age").OrderBy("id").Get(ctx, &rows))
	assert.Equal(t, "Ann", rows[0]["name"]) // text columns arrive as string, not []byte
	assert.Nil(t, rows[3]["age"])

	var u User
	require.NoError(t, users().Where("name", "Bob").Sole(ctx, &u))
	assert.Equal(t, db.ErrMultipleRecords, users().Where("age", ">", 0).Sole(ctx, &u))
	assert.Equal(t, db.ErrNotFound, users().Where("age", 999).First(ctx, &u))
	require.NoError(t, users().Find(ctx, 3, &u))
	assert.Equal(t, "Cy", u.Name)

	var ids map[int64]string
	require.NoError(t, users().PluckMap(ctx, "name", "id", &ids))
	assert.Equal(t, map[int64]string{1: "Ann", 2: "Bob", 3: "Cy", 4: "Di"}, ids)

	ok, err := users().Where("name", "Ann").Exists(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	no, err := users().Where("name", "zzz").DoesntExist(ctx)
	require.NoError(t, err)
	assert.True(t, no)
}

func TestMySQLAggregates(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	n, err := conn.Table("users").Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(4), n)
	n, err = conn.Table("users").Count(ctx, "age")
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	sum, err := conn.Table("users").Sum(ctx, "age")
	require.NoError(t, err)
	assert.Equal(t, 96.0, sum)
	avg, err := conn.Table("users").Avg(ctx, "age")
	require.NoError(t, err)
	assert.Equal(t, 32.0, avg)
	var mn, mx int
	require.NoError(t, conn.Table("users").Min(ctx, "age", &mn))
	require.NoError(t, conn.Table("users").Max(ctx, "age", &mx))
	assert.Equal(t, [2]int{25, 41}, [2]int{mn, mx})
	none, err := conn.Table("users").Where("id", 99).Sum(ctx, "age")
	require.NoError(t, err)
	assert.Equal(t, 0.0, none)

	// wrapped forms: grouped, distinct, limited, joined. Strict servers need the group column selected.
	n, err = conn.Table("posts").Select("user_id").GroupBy("user_id").Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	n, err = conn.Table("posts").Select("user_id").Distinct().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), n)
	n, err = conn.Table("users").OrderBy("id").Limit(2).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	n, err = conn.Table("posts").Join("users", "users.id", "posts.user_id").Where("posts.views", ">", 0).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(4), n)
	total, err := conn.Table("posts").OrderBy("id").Limit(3).Sum(ctx, "posts.views")
	require.NoError(t, err)
	assert.Equal(t, 22.0, total)
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
		{
			"group",
			users().Where("active", true).
				WhereGroup(func(q *db.Builder) { q.Where("age", ">", 35).OrWhereNull("age") }),
			[]string{"Cy", "Di"},
		},
		{"not", users().WhereNot("active", true), []string{"Bob"}},
		{
			"exists",
			users().WhereExists(posts().SelectRaw("1").WhereColumn("posts.user_id", "users.id").Where("views", ">", 6)),
			[]string{"Ann", "Bob"},
		},
		{
			"not exists",
			users().WhereNotExists(posts().SelectRaw("1").WhereColumn("posts.user_id", "users.id")),
			[]string{"Di"},
		},
		{
			"in subquery",
			users().WhereIn("id", posts().Select("user_id").Where("published", true)),
			[]string{"Ann", "Bob", "Cy"},
		},
		{"scalar subquery", users().Where("age", ">", conn.Table("users").SelectRaw("avg(age)")), []string{"Cy"}},
		{"any", users().WhereAny([]string{"name", "email"}, "like", "b%"), []string{"Bob"}},
		{"all", users().WhereAll([]string{"name", "email"}, "like", "%i%"), []string{"Di"}},
		{"none", users().WhereNone([]string{"name", "email"}, "like", "%o%"), []string{"Ann", "Cy", "Di"}},
		{
			"year month day",
			users().WhereYear("created_at", 2024).WhereMonth("created_at", 3).WhereDay("created_at", ">", 7),
			[]string{"Bob"},
		},
		{
			"date and time",
			users().WhereDate("created_at", "2024-03-07").WhereTime("created_at", ">=", "09:00:00"),
			[]string{"Ann"},
		},
		{
			"date from time.Time",
			users().WhereDate("created_at", time.Date(2024, 3, 8, 23, 0, 0, 0, time.UTC)),
			[]string{"Bob"},
		},
		{"row values", users().WhereRowValues([]string{"id", "age"}, "=", []any{1, 30}), []string{"Ann"}},
		{"raw", users().WhereRaw("length(name) = ?", 2), []string{"Cy", "Di"}},
		{"map", users().WhereMap(map[string]any{"active": true, "name": "Cy"}), []string{"Cy"}},
		{"latest limit offset", conn.Table("users").Latest("id").Limit(2).Offset(1), []string{"Cy", "Bob"}},
		{
			"left join with value",
			users().LeftJoinFunc("posts", func(j *db.JoinClause) { j.On("posts.user_id", "users.id").Where("posts.views", ">", 8) }).
				WhereNull("posts.id"),
			[]string{"Bob", "Cy", "Di"},
		},
		{
			"join sub",
			users().JoinSub(posts().Select("user_id").SelectRaw("max(views) as mv").GroupBy("user_id"), "m", "m.user_id", "users.id").
				Where("m.mv", ">", 6),
			[]string{"Ann", "Bob"},
		},
		{
			"cross join",
			conn.Table("users").CrossJoin("posts").Where("users.id", 1).Where("posts.id", 1),
			[]string{"Ann"},
		},
		{
			"from sub",
			conn.NewQuery().FromSub(conn.Table("users").Where("active", true), "au").OrderBy("name"),
			[]string{"Ann", "Cy", "Di"},
		},
		{
			"union",
			conn.Table("users").
				Select("name").
				Where("id", 1).
				Union(conn.Table("users").Select("name").Where("id", 2)).
				OrderBy("name", "desc"),
			[]string{"Bob", "Ann"},
		},
		{
			"union all limit",
			conn.Table("users").
				Select("name").
				Where("id", 1).
				UnionAll(conn.Table("users").Select("name").Where("id", 1)).
				Limit(5),
			[]string{"Ann", "Ann"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pluckNames(t, tc.q)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			assert.Equal(t, tc.want, got)
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
			require.NoError(t, q.Get(ctx, &rows))
		})
	}

	t.Run("select sub value", func(t *testing.T) {
		var rows []map[string]any
		require.NoError(t, conn.Table("users").Select("name").
			SelectSub(posts().SelectRaw("count(*)").WhereColumn("posts.user_id", "users.id"), "pc").
			OrderBy("id").Get(ctx, &rows))
		assert.Equal(t, "2", fmt.Sprint(rows[0]["pc"]))
		assert.Equal(t, "0", fmt.Sprint(rows[3]["pc"]))
	})
}

func TestMySQLDialectSpecificJoinsAndLocks(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)

	t.Run("lateral join", func(t *testing.T) {
		latest := conn.Table("posts").
			Select("title").
			WhereColumn("posts.user_id", "users.id").
			OrderBy("views", "desc").
			Limit(1)
		got := pluckNames(t, conn.Table("users").JoinLateral(latest, "lp").OrderBy("users.id"))
		assert.Equal(t, []string{"Ann", "Bob", "Cy"}, got)
		got = pluckNames(t, conn.Table("users").LeftJoinLateral(latest, "lp").OrderBy("users.id"))
		assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, got)
	})
	t.Run("row locks run inside a transaction", func(t *testing.T) {
		require.NoError(t, conn.Transaction(ctx, func(tx *db.DB) error {
			var u User
			if err := tx.Table("users").Where("id", 1).LockForUpdate().First(ctx, &u); err != nil {
				return err
			}
			return tx.Table("users").Where("id", 2).SharedLock().First(ctx, &u)
		}))
	})
	t.Run("custom lock clause", func(t *testing.T) {
		clause := "for update"
		require.NoError(t, conn.Transaction(ctx, func(tx *db.DB) error {
			var u User
			return tx.Table("users").Where("id", 1).Lock(clause).First(ctx, &u)
		}))
	})
	t.Run("straight join and index hints", func(t *testing.T) {
		got := pluckNames(
			t,
			conn.Table("users").StraightJoin("posts", "posts.user_id", "users.id").Where("posts.id", 1),
		)
		assert.Equal(t, []string{"Ann"}, got)
		for _, q := range []*db.Builder{
			conn.Table("users").UseIndex("PRIMARY"),
			conn.Table("users").ForceIndex("PRIMARY"),
			conn.Table("users").IgnoreIndex("email"),
		} {
			var rows []map[string]any
			require.NoError(t, q.Where("id", 1).Get(ctx, &rows))
			assert.Len(t, rows, 1)
		}
	})
}

func TestMySQLJSON(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	ordered := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	assert.Equal(t, []string{"Ann", "Bob"}, pluckNames(t, ordered().WhereJsonContains("meta->tags", "b")))
	assert.Equal(t, []string{"Ann"}, pluckNames(t, ordered().WhereJsonContains("meta->tags", "a")))
	assert.Equal(
		t,
		[]string{"Bob"},
		pluckNames(t, ordered().WhereJsonContains("meta", map[string]any{"theme": "light"})),
	)
	// a missing path makes json_contains / @> NULL on Postgres and MySQL, so "doesn't contain" skips Cy
	// (SQLite's json_each form returns true there).
	assert.Equal(
		t,
		[]string{"Bob"},
		pluckNames(t, ordered().WhereJsonDoesntContain("meta->tags", "a").WhereNotNull("meta")),
	)
	assert.Equal(t, []string{"Ann", "Bob"}, pluckNames(t, ordered().WhereJsonContainsKey("meta->theme")))
	assert.Equal(
		t,
		[]string{"Cy"},
		pluckNames(t, ordered().WhereJsonDoesntContainKey("meta->theme").WhereNotNull("meta")),
	)
	assert.Equal(t, []string{"Ann"}, pluckNames(t, ordered().WhereJsonLength("meta->tags", ">", 1)))
	assert.Equal(t, []string{"Bob"}, pluckNames(t, ordered().Where("meta->theme", "light")))

	var theme string
	require.NoError(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->theme", &theme))
	assert.Equal(t, "dark", theme)

	// arrays and objects come back as JSON text, not NULL (MariaDB's json_value would drop them)
	var tags, flags string
	require.NoError(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->tags", &tags))
	assert.Equal(t, `["a", "b"]`, tags)
	require.NoError(t, conn.Table("users").Where("id", 1).Value(ctx, "meta", &flags))
	assert.Contains(t, flags, `"theme": "dark"`)

	assert.Equal(t, []string{"Ann"}, pluckNames(t, ordered().WhereJsonOverlaps("meta->tags", []string{"a", "z"})))
	assert.Equal(
		t,
		[]string{"Bob"},
		pluckNames(
			t,
			ordered().WhereJsonDoesntOverlap("meta->tags", []string{"a", "z"}).
				WhereNotNull("meta").
				WhereJsonContainsKey("meta->tags"),
		),
	)

	t.Run("updates set paths and keep siblings", func(t *testing.T) {
		n, err := conn.Table("users").
			Where("id", 1).
			Update(ctx, map[string]any{"meta->theme": "blue", "meta->beta": true})
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		require.NoError(t, conn.Table("users").Where("id", 1).Value(ctx, "meta->theme", &theme))
		assert.Equal(t, "blue", theme)
		assert.Equal(t, []string{"Ann"}, pluckNames(t, ordered().Where("meta->beta", true)))
		assert.Equal(t, []string{"Ann"}, pluckNames(t, ordered().WhereJsonContains("meta->tags", "a")))
	})
	t.Run("container value", func(t *testing.T) {
		_, err := conn.Table("users").Where("id", 2).Update(ctx, map[string]any{"meta->tags": []string{"x", "y"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"Bob"}, pluckNames(t, ordered().WhereJsonContains("meta->tags", "y")))
	})
}

func TestMySQLWrites(t *testing.T) {
	s := startMySQL(t)
	t.Run("insert variants", func(t *testing.T) {
		conn := s.reset(t)
		n, err := conn.Table("users").
			Insert(ctx, map[string]any{"name": "Eve", "email": "eve@x"}, map[string]any{"name": "Fay", "email": "fay@x"})
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		id, err := conn.Table("users").InsertGetID(ctx, map[string]any{"name": "Gus", "email": "gus@x"})
		require.NoError(t, err)
		assert.Equal(t, int64(7), id)
		n, err = conn.Table("users").InsertOrIgnore(ctx, map[string]any{"name": "Dup", "email": "ann@x"})
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		n, err = conn.Table("posts").InsertUsing(ctx, []string{"user_id", "title"},
			conn.Table("users").Select("id", "name").Where("id", "<=", 2))
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Table("users").InsertOrIgnoreUsing(ctx, []string{"name", "email"},
			conn.Table("users").Select("name", "email").Where("id", 1))
		require.NoError(t, err)
		assert.Equal(t, int64(0), n)
		_, err = conn.Table("users").Insert(ctx, map[string]any{"name": "Ann2", "email": "ann@x"})
		require.Error(t, err)
	})
	t.Run("upsert and update or insert", func(t *testing.T) {
		conn := s.reset(t)
		_, err := conn.Table("users").Upsert(ctx, []map[string]any{
			{"name": "Ann!", "email": "ann@x", "age": 31},
			{"name": "New", "email": "new@x", "age": 1},
		}, []string{"email"}, []string{"name", "age"})
		require.NoError(t, err)
		var name string
		require.NoError(t, conn.Table("users").Where("email", "ann@x").Value(ctx, "name", &name))
		assert.Equal(t, "Ann!", name)
		c, _ := conn.Table("users").Count(ctx)
		assert.Equal(t, int64(5), c)
		for _, v := range []string{"Zed", "Zed2"} {
			ok, err := conn.Table("users").
				UpdateOrInsert(ctx, map[string]any{"email": "zed@x"}, map[string]any{"name": v})
			require.NoError(t, err)
			assert.True(t, ok)
		}
		require.NoError(t, conn.Table("users").Where("email", "zed@x").Value(ctx, "name", &name))
		assert.Equal(t, "Zed2", name)
	})
	t.Run("update increment guard", func(t *testing.T) {
		conn := s.reset(t)
		n, err := conn.Table("posts").Where("user_id", 1).Increment(ctx, "views", 5, map[string]any{"title": "bumped"})
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Table("posts").Where("id", 1).Decrement(ctx, "views", 3, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		n, err = conn.Table("posts").Where("id", 2).IncrementEach(ctx, map[string]any{"views": 1, "user_id": 1}, nil)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		var v int
		require.NoError(t, conn.Table("posts").Where("id", 1).Value(ctx, "views", &v))
		assert.Equal(t, 12, v) // 10 + 5 - 3
		_, err = conn.Table("users").Update(ctx, map[string]any{"bio": "x"})
		assert.Equal(t, db.ErrNoWhere, err)
		n, err = conn.Table("users").AllRows().Update(ctx, map[string]any{"bio": "all"})
		require.NoError(t, err)
		assert.Equal(t, int64(4), n)
	})
	t.Run("update and delete with join or limit", func(t *testing.T) {
		conn := s.reset(t)
		n, err := conn.Table("users as u").
			Join("posts as p", "p.user_id", "u.id").
			Where("p.views", ">", 6).
			Update(ctx, map[string]any{"u.score": 9})
			// MySQL counts changed rows, so pick a column that changes
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
		n, err = conn.Table("posts").
			Where("views", ">", 0).
			OrderBy("views", "desc").
			Limit(1).
			Update(ctx, map[string]any{"title": "top"})
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		var title string
		require.NoError(t, conn.Table("posts").Where("views", 10).Value(ctx, "title", &title))
		assert.Equal(t, "top", title)

		n, err = conn.Table("posts").Where("published", false).Delete(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		n, err = conn.Table("posts").Where("views", ">", 0).OrderBy("views").Limit(1).Delete(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		n, err = conn.Table("posts as p").Join("users as u", "u.id", "p.user_id").Where("u.name", "Bob").Delete(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		_, err = conn.Table("posts").Delete(ctx)
		assert.Equal(t, db.ErrNoWhere, err)
		n, err = conn.Table("posts").AllRows().Delete(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
	})
	t.Run("truncate resets identity", func(t *testing.T) {
		conn := s.reset(t)
		require.NoError(t, conn.Table("posts").Truncate(ctx))
		c, _ := conn.Table("posts").Count(ctx)
		assert.Equal(t, int64(0), c)
		id, err := conn.Table("posts").InsertGetID(ctx, map[string]any{"title": "fresh"})
		require.NoError(t, err)
		assert.Equal(t, int64(1), id)
	})
	t.Run("returning", func(t *testing.T) {
		conn := s.reset(t)
		var out []struct {
			ID   int64
			Name string
		}
		err := conn.Table("users").
			Returning("id", "name").
			InsertReturning(ctx, &out, map[string]any{"name": "Rae", "email": "rae@x"})
		assert.ErrorIs(t, err, db.ErrUnsupported)
	})
	t.Run("update from", func(t *testing.T) {
		conn := s.reset(t)
		q := conn.Table("users").Join("posts", "posts.user_id", "users.id").Where("posts.views", ">", 9)
		_, err := q.UpdateFrom(ctx, map[string]any{"bio": db.Raw("`posts`.`title`")})
		assert.ErrorIs(t, err, db.ErrUnsupported)
	})
}

func TestMySQLPaginationAndChunking(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	var us []User
	p, err := users().Paginate(ctx, 2, 3, &us)
	require.NoError(t, err)
	assert.Equal(t, db.Page{Total: 4, PerPage: 3, CurrentPage: 2, LastPage: 2, From: 4, To: 4}, p)
	assert.Equal(t, []string{"Di"}, names(us))
	sp, err := users().SimplePaginate(ctx, 1, 3, &us)
	require.NoError(t, err)
	assert.Equal(t, db.SimplePage{PerPage: 3, CurrentPage: 1, HasMore: true}, sp)

	var seen []string
	cursor := ""
	for range 5 {
		var page []User
		cp, pageErr := conn.Table("users").CursorPaginate(ctx, 3, cursor, &page)
		require.NoError(t, pageErr)
		seen = append(seen, names(page)...)
		if !cp.HasMore {
			break
		}
		cursor = cp.NextCursor
	}
	assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, seen)

	var pages [][]string
	require.NoError(
		t,
		users().Chunk(ctx, 3, &us, func() (bool, error) { pages = append(pages, names(us)); return true, nil }),
	)
	assert.Equal(t, [][]string{{"Ann", "Bob", "Cy"}, {"Di"}}, pages)

	var total int
	require.NoError(
		t,
		db.As[map[string]any](conn.Table("posts")).ChunkByID(ctx, 2, func(rows []map[string]any) (bool, error) {
			total += len(rows)
			for _, r := range rows {
				if _, delErr := conn.Table("posts").Where("id", r["id"]).Delete(ctx); delErr != nil {
					return false, delErr
				}
			}
			return true, nil
		}),
	)
	assert.Equal(t, 4, total)

	var got []string
	for u, err := range db.Stream[User](ctx, users()) {
		require.NoError(t, err)
		got = append(got, u.Name)
		if u.Name == "Bob" {
			break
		}
	}
	assert.Equal(t, []string{"Ann", "Bob"}, got)
	c, err := conn.Table("users").Count(ctx) // pool still healthy after early exit
	require.NoError(t, err)
	assert.Equal(t, int64(4), c)
}

func TestMySQLTransactions(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	count := func() int64 {
		n, err := conn.Table("users").Count(ctx)
		require.NoError(t, err)
		return n
	}
	insert := func(tx *db.DB, name string) error {
		_, err := tx.Table("users").Insert(ctx, map[string]any{"name": name, "email": name + "@x"})
		return err
	}

	require.NoError(t, conn.Transaction(ctx, func(tx *db.DB) error { return insert(tx, "c1") }))
	assert.Equal(t, int64(5), count())

	boom := errors.New("boom")
	assert.Equal(t, boom, conn.Transaction(ctx, func(tx *db.DB) error {
		if err := insert(tx, "r1"); err != nil {
			return err
		}
		return boom
	}))
	assert.Equal(t, int64(5), count())

	func() {
		defer func() { assert.Equal(t, any("kaboom"), recover()) }()
		_ = conn.Transaction(ctx, func(tx *db.DB) error {
			_ = insert(tx, "p1")
			panic("kaboom")
		})
	}()
	assert.Equal(t, int64(5), count())

	require.NoError(t, conn.Transaction(ctx, func(tx *db.DB) error {
		if err := insert(tx, "outer"); err != nil {
			return err
		}
		// a real SQL error inside a savepoint must not poison the outer transaction
		err := tx.Transaction(ctx, func(inner *db.DB) error {
			return insert(inner, "outer") // duplicate email
		})
		require.Error(t, err)
		return tx.Transaction(ctx, func(inner *db.DB) error { return insert(inner, "inner2") })
	}))
	var got []string
	require.NoError(t, conn.Table("users").Where("name", "like", "%er%").OrderBy("id").Pluck(ctx, "name", &got))
	assert.Equal(t, []string{"outer", "inner2"}, got)
	assert.Equal(t, int64(7), count())

	opts := &sql.TxOptions{ReadOnly: true}
	err := conn.TransactionOpts(ctx, opts, func(tx *db.DB) error { return insert(tx, "ro") })
	require.Error(t, err)
	assert.Equal(t, int64(7), count())
}

func TestMySQLTypedReadsAndStructWrites(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)
	users := func() *db.Builder { return conn.Table("users").OrderBy("id") }

	us, err := db.As[User](users()).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, names(us))
	u, err := db.As[User](conn.Table("users")).Find(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, "Cy", u.Name)
	_, err = db.As[User](users().Where("name", "nobody")).First(ctx)
	require.ErrorIs(t, err, db.ErrNotFound)

	ids, err := db.As[int64](conn.Table("users").Select("id").OrderBy("id")).Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2, 3, 4}, ids)
	mx, err := db.MaxAs[int](ctx, conn.Table("users"), "age")
	require.NoError(t, err)
	assert.Equal(t, 41, mx)

	p, err := db.As[User](users()).Paginate(ctx, 2, 3)
	require.NoError(t, err)
	assert.Equal(t, [3]any{int64(4), 2, "Di"}, [3]any{p.Total, p.LastPage, names(p.Items)[0]})
	var seen []string
	cursor := ""
	for range 5 {
		cp, pageErr := db.As[User](conn.Table("users")).CursorPaginate(ctx, 3, cursor)
		require.NoError(t, pageErr)
		seen = append(seen, names(cp.Items)...)
		if !cp.HasMore {
			break
		}
		cursor = cp.NextCursor
	}
	assert.Equal(t, []string{"Ann", "Bob", "Cy", "Di"}, seen)

	// struct writes
	n, err := db.As[Member](
		conn.Table("users"),
	).Insert(ctx, Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")})
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	id, err := db.As[Member](conn.Table("users")).InsertGetID(ctx, Member{Name: "Gus", Email: ptr("gus@x")})
	require.NoError(t, err)
	assert.Equal(t, int64(7), id)
	n, err = db.As[Member](conn.Table("users")).InsertOrIgnore(ctx, Member{Name: "Dup", Email: ptr("ann@x")})
	require.NoError(t, err)
	assert.Equal(t, int64(0), n)
	_, err = db.As[Member](conn.Table("users")).Upsert(ctx, []Member{{Name: "Ann!", Email: ptr("ann@x"), Age: ptr(31)}},
		[]string{"email"}, []string{"name", "age"})
	require.NoError(t, err)
	_, err = db.As[Member](conn.Table("users").Where("id", 2)).Update(ctx, Member{Bio: "set"})
	require.NoError(t, err)
	var rows []User
	require.NoError(t, conn.Table("users").WhereIn("id", []int{1, 2}).OrderBy("id").Get(ctx, &rows))
	assert.Equal(t, [3]any{"Ann!", "set", "Bob"}, [3]any{rows[0].Name, rows[1].Bio, rows[1].Name}) // Bob's name kept

	// MySQL has no RETURNING
	_, err = db.As[Member](conn.Table("users")).InsertReturning(ctx, Member{Name: "Rae", Email: ptr("rae@x")})
	assert.ErrorIs(t, err, db.ErrUnsupported)
}

func TestMySQLFromTakesTheTableFromTheType(t *testing.T) {
	s := startMySQL(t)
	conn := s.reset(t)

	us, err := db.From[User](conn).Where("active", true).OrderBy("id").Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"Ann", "Cy", "Di"}, names(us))
	u, err := db.From[User](conn).Find(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, "Bob", u.Name)
	n, err := db.From[User](conn).Where("age", ">", 26).Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	sum, err := db.From[Post](conn).Sum(ctx, "views")
	require.NoError(t, err)
	assert.Equal(t, 23.0, sum)
	p, err := db.From[User](conn).OrderBy("id").Paginate(ctx, 2, 3)
	require.NoError(t, err)
	assert.Equal(t, [2]any{int64(4), "Di"}, [2]any{p.Total, names(p.Items)[0]})

	cnt, err := db.From[Member](
		conn,
	).Insert(ctx, Member{Name: "Eve", Email: ptr("eve@x")}, Member{Name: "Fay", Email: ptr("fay@x")})
	require.NoError(t, err)
	assert.Equal(t, int64(2), cnt)
	id, err := db.From[Member](conn).InsertGetID(ctx, Member{Name: "Gus", Email: ptr("gus@x")})
	require.NoError(t, err)
	assert.Equal(t, int64(7), id)
	cnt, err = db.From[Member](conn).Where("id", 1).Update(ctx, Member{Bio: "set"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), cnt)
	_, err = db.From[Member](conn).Update(ctx, Member{Name: "x"})
	assert.Equal(t, db.ErrNoWhere, err)
	cnt, err = db.From[Member](conn).Where("name", "Fay").Delete(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), cnt)

	require.NoError(t, db.From[Post](conn).Truncate(ctx))
	pid, err := db.From[Post](conn).InsertGetID(ctx, Post{Title: "fresh"})
	require.NoError(t, err)
	assert.Equal(t, int64(1), pid)

	_, err = db.From[Untitled](conn).Get(ctx)
	assert.ErrorIs(t, err, db.ErrInvalidArgs)
}

func TestMySQLCTE(t *testing.T) {
	conn := startMySQL(t).reset(t)
	active := func() *db.Builder { return conn.Table("users").Where("active", true) }

	t.Run("plain", func(t *testing.T) {
		assert.Equal(
			t,
			[]string{"Ann", "Cy", "Di"},
			pluckNames(t, conn.Table("au").With("au", active()).OrderBy("name")),
		)
	})
	t.Run("recursive series", func(t *testing.T) {
		anchor := conn.Table("users").SelectRaw("1 as n").Where("id", 1)
		step := conn.Table("seq").SelectRaw("n + 1").Where("n", "<", 5)
		var got []int64
		require.NoError(
			t,
			conn.Table("seq").WithRecursive("seq", anchor.UnionAll(step), "n").OrderBy("n").Pluck(ctx, "n", &got),
		)
		assert.Equal(t, []int64{1, 2, 3, 4, 5}, got)
	})
	t.Run("inside where in", func(t *testing.T) {
		var got []string
		sub := conn.Table("au").Select("id").With("au", active())
		require.NoError(t, conn.Table("posts").WhereIn("user_id", sub).OrderBy("title").Pluck(ctx, "title", &got))
		assert.Equal(t, []string{"p1", "p2", "p4"}, got)
	})
	t.Run("with an outer union", func(t *testing.T) {
		q := conn.Table("au").Select("name").With("au", active()).Where("name", "Ann").
			UnionAll(conn.Table("users").Select("name").Where("name", "Bob")).OrderBy("name")
		assert.Equal(t, []string{"Ann", "Bob"}, pluckNames(t, q))
	})
	t.Run("count and exists", func(t *testing.T) {
		n, err := conn.Table("au").With("au", active()).Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(3), n)
		ok, err := conn.Table("au").With("au", active()).Where("name", "Bob").Exists(ctx)
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("insert using", func(t *testing.T) {
		sub := conn.Table("inactive").Select("name").With("inactive", conn.Table("users").Where("active", false))
		n, err := conn.Table("users").InsertUsing(ctx, []string{"name"}, sub)
		require.NoError(t, err)
		assert.Equal(t, int64(1), n)
		c, err := conn.Table("users").Where("name", "Bob").Count(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(2), c)
	})
}
