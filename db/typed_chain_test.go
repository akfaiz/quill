package db_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/akfaiz/quill/db"
)

type chainRow struct{ ID int64 }

func (chainRow) TableName() string { return "t" }

// TestTypedChainMatchesBuilder runs every chain method on the typed query and on
// a plain Builder and requires the same SQL and arguments.
func TestTypedChainMatchesBuilder(t *testing.T) {
	sub := func() *db.Builder { return db.Use(db.Postgres).Table("s").Where("k", 1) }
	type pair struct {
		name  string
		typed func(*db.Query[chainRow]) *db.Query[chainRow]
		plain func(*db.Builder) *db.Builder
	}
	pairs := []pair{
		{"Select", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Select("a", "b") }, func(b *db.Builder) *db.Builder { return b.Select("a", "b") }},
		{"AddSelect", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Select("a").AddSelect("b") }, func(b *db.Builder) *db.Builder { return b.Select("a").AddSelect("b") }},
		{"SelectRaw", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.SelectRaw("count(*) + ?", 1) }, func(b *db.Builder) *db.Builder { return b.SelectRaw("count(*) + ?", 1) }},
		{"Distinct", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Distinct() }, func(b *db.Builder) *db.Builder { return b.Distinct() }},
		{"Where", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Where("a", ">", 1) }, func(b *db.Builder) *db.Builder { return b.Where("a", ">", 1) }},
		{"OrWhere", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Where("a", 1).OrWhere("b", 2) }, func(b *db.Builder) *db.Builder { return b.Where("a", 1).OrWhere("b", 2) }},
		{"WhereNot", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereNot("a", 1) }, func(b *db.Builder) *db.Builder { return b.WhereNot("a", 1) }},
		{"WhereRaw", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereRaw("a = ?", 1) }, func(b *db.Builder) *db.Builder { return b.WhereRaw("a = ?", 1) }},
		{"OrWhereRaw", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Where("a", 1).OrWhereRaw("b = ?", 2) }, func(b *db.Builder) *db.Builder { return b.Where("a", 1).OrWhereRaw("b = ?", 2) }},
		{"WhereColumn", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereColumn("a", "<", "b") }, func(b *db.Builder) *db.Builder { return b.WhereColumn("a", "<", "b") }},
		{"WhereIn", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereIn("a", []int{1, 2}) }, func(b *db.Builder) *db.Builder { return b.WhereIn("a", []int{1, 2}) }},
		{"OrWhereIn", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Where("x", 0).OrWhereIn("a", sub()) }, func(b *db.Builder) *db.Builder { return b.Where("x", 0).OrWhereIn("a", sub()) }},
		{"WhereNotIn", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereNotIn("a", []int{1}) }, func(b *db.Builder) *db.Builder { return b.WhereNotIn("a", []int{1}) }},
		{"WhereNull", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereNull("a") }, func(b *db.Builder) *db.Builder { return b.WhereNull("a") }},
		{"OrWhereNull", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Where("x", 0).OrWhereNull("a") }, func(b *db.Builder) *db.Builder { return b.Where("x", 0).OrWhereNull("a") }},
		{"WhereNotNull", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereNotNull("a") }, func(b *db.Builder) *db.Builder { return b.WhereNotNull("a") }},
		{"WhereBetween", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereBetween("a", 1, 5) }, func(b *db.Builder) *db.Builder { return b.WhereBetween("a", 1, 5) }},
		{"WhereNotBetween", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereNotBetween("a", 1, 5) }, func(b *db.Builder) *db.Builder { return b.WhereNotBetween("a", 1, 5) }},
		{"WhereLike", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereLike("a", "x%", true) }, func(b *db.Builder) *db.Builder { return b.WhereLike("a", "x%", true) }},
		{"WhereGroup", func(q *db.Query[chainRow]) *db.Query[chainRow] {
			return q.WhereGroup(func(g *db.Builder) { g.Where("a", 1).OrWhere("b", 2) })
		}, func(b *db.Builder) *db.Builder {
			return b.WhereGroup(func(g *db.Builder) { g.Where("a", 1).OrWhere("b", 2) })
		}},
		{"OrWhereGroup", func(q *db.Query[chainRow]) *db.Query[chainRow] {
			return q.Where("x", 0).OrWhereGroup(func(g *db.Builder) { g.Where("a", 1) })
		}, func(b *db.Builder) *db.Builder {
			return b.Where("x", 0).OrWhereGroup(func(g *db.Builder) { g.Where("a", 1) })
		}},
		{"WhereExists", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereExists(sub()) }, func(b *db.Builder) *db.Builder { return b.WhereExists(sub()) }},
		{"WhereNotExists", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.WhereNotExists(sub()) }, func(b *db.Builder) *db.Builder { return b.WhereNotExists(sub()) }},
		{"Join", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Join("j", "j.a", "t.a") }, func(b *db.Builder) *db.Builder { return b.Join("j", "j.a", "t.a") }},
		{"LeftJoin", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.LeftJoin("j", "j.a", "=", "t.a") }, func(b *db.Builder) *db.Builder { return b.LeftJoin("j", "j.a", "=", "t.a") }},
		{"RightJoin", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.RightJoin("j", "j.a", "t.a") }, func(b *db.Builder) *db.Builder { return b.RightJoin("j", "j.a", "t.a") }},
		{"JoinFunc", func(q *db.Query[chainRow]) *db.Query[chainRow] {
			return q.JoinFunc("j", func(j *db.JoinClause) { j.On("j.a", "t.a").Where("j.k", 1) })
		}, func(b *db.Builder) *db.Builder {
			return b.JoinFunc("j", func(j *db.JoinClause) { j.On("j.a", "t.a").Where("j.k", 1) })
		}},
		{"LeftJoinFunc", func(q *db.Query[chainRow]) *db.Query[chainRow] {
			return q.LeftJoinFunc("j", func(j *db.JoinClause) { j.On("j.a", "t.a") })
		}, func(b *db.Builder) *db.Builder {
			return b.LeftJoinFunc("j", func(j *db.JoinClause) { j.On("j.a", "t.a") })
		}},
		{"GroupBy", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.GroupBy("a", "b") }, func(b *db.Builder) *db.Builder { return b.GroupBy("a", "b") }},
		{"Having", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.GroupBy("a").Having("n", ">", 1) }, func(b *db.Builder) *db.Builder { return b.GroupBy("a").Having("n", ">", 1) }},
		{"HavingRaw", func(q *db.Query[chainRow]) *db.Query[chainRow] {
			return q.GroupBy("a").HavingRaw("count(*) > ?", 1)
		}, func(b *db.Builder) *db.Builder { return b.GroupBy("a").HavingRaw("count(*) > ?", 1) }},
		{"OrderBy", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.OrderBy("a", "desc") }, func(b *db.Builder) *db.Builder { return b.OrderBy("a", "desc") }},
		{"OrderByDesc", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.OrderByDesc("a") }, func(b *db.Builder) *db.Builder { return b.OrderByDesc("a") }},
		{"OrderByRaw", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.OrderByRaw("random()") }, func(b *db.Builder) *db.Builder { return b.OrderByRaw("random()") }},
		{"Latest", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Latest() }, func(b *db.Builder) *db.Builder { return b.Latest() }},
		{"Oldest", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Oldest("a") }, func(b *db.Builder) *db.Builder { return b.Oldest("a") }},
		{"Limit", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Limit(5) }, func(b *db.Builder) *db.Builder { return b.Limit(5) }},
		{"Offset", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Offset(5) }, func(b *db.Builder) *db.Builder { return b.Offset(5) }},
		{"ForPage", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.ForPage(3, 10) }, func(b *db.Builder) *db.Builder { return b.ForPage(3, 10) }},
		{"LockForUpdate", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.LockForUpdate() }, func(b *db.Builder) *db.Builder { return b.LockForUpdate() }},
		{"SharedLock", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.SharedLock() }, func(b *db.Builder) *db.Builder { return b.SharedLock() }},
		{"AllRows", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.AllRows() }, func(b *db.Builder) *db.Builder { return b.AllRows() }},
		{"Returning", func(q *db.Query[chainRow]) *db.Query[chainRow] { return q.Returning("id") }, func(b *db.Builder) *db.Builder { return b.Returning("id") }},
	}

	conn := db.Use(db.Postgres)
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			typed := p.typed(db.From[chainRow](conn))
			gotSQL, gotArgs, gotErr := typed.Builder().ToSQL()
			wantSQL, wantArgs, wantErr := p.plain(conn.Table("t")).ToSQL()
			if (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("errors differ: typed %v, builder %v", gotErr, wantErr)
			}
			if gotSQL != wantSQL || !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Errorf("typed   %s %v\nbuilder %s %v", gotSQL, gotArgs, wantSQL, wantArgs)
			}
		})
	}

	// every method that returns the query (except the helpers below) must be in the table
	covered := map[string]bool{}
	for _, p := range pairs {
		covered[p.name] = true
	}
	helpers := []string{"Apply", "When", "Unless", "Clone"}
	qt := reflect.TypeFor[*db.Query[chainRow]]()
	for i := 0; i < qt.NumMethod(); i++ {
		m := qt.Method(i)
		if m.Type.NumOut() == 1 && m.Type.Out(0) == qt && !slices.Contains(helpers, m.Name) && !covered[m.Name] {
			t.Errorf("chain method %s has no parity case", m.Name)
		}
	}
}

func TestTypedChainHelpers(t *testing.T) {
	conn := db.Use(db.Postgres)
	sql := func(q *db.Query[chainRow]) string {
		s, _, err := q.Builder().ToSQL()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	q := db.From[chainRow](conn).
		When(true, func(q *db.Query[chainRow]) { q.Where("a", 1) }).
		When(false, func(q *db.Query[chainRow]) { q.Where("never", 1) }).
		Unless(false, func(q *db.Query[chainRow]) { q.Where("b", 2) }).
		Unless(true, func(q *db.Query[chainRow]) { q.Where("never", 2) }).
		Apply(func(b *db.Builder) { b.WhereDate("created_at", "2024-01-02") })
	if want := `select * from "t" where "a" = $1 and "b" = $2 and "created_at"::date = $3`; sql(q) != want {
		t.Errorf("got %s\nwant %s", sql(q), want)
	}
	c := q.Clone().Limit(1)
	if sql(q) == sql(c) || sql(c) != sql(q)+" limit 1" {
		t.Errorf("clone must be independent:\n q: %s\n c: %s", sql(q), sql(c))
	}
}
