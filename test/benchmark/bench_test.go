package benchmark

import (
	"testing"

	sq "github.com/Masterminds/squirrel"
	"github.com/doug-martin/goqu/v9"
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
	"github.com/huandu/go-sqlbuilder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

// build turns one query into SQL text and bind arguments.
type build func() (string, []any, error)

// scenario is the same query written for each library. All of them target
// PostgreSQL with bound ($n) placeholders, which is what a real application sends.
type scenario struct {
	name   string
	quill  build
	squirr build
	goqu   build
	sqlb   build

	// limitArgs is how many extra arguments goqu and sqlbuilder bind: unlike quill
	// and squirrel they send LIMIT and OFFSET as parameters instead of literals.
	limitArgs int
}

var (
	pg      = db.Use(db.Postgres)
	goquPG  = goqu.Dialect("postgres")
	sqPG    = sq.StatementBuilder.PlaceholderFormat(sq.Dollar)
	ids     = []int{1, 2, 3, 4, 5}
	idsAny  = []any{1, 2, 3, 4, 5}
	insRows = [][3]any{{"ann", 30, "a@x"}, {"bob", 25, "b@x"}, {"cy", 41, "c@x"}}
)

var scenarios = []scenario{
	{
		name:      "SimpleSelect",
		limitArgs: 1,
		quill: func() (string, []any, error) {
			return pg.Table("users").Select("id", "name").Where("votes", ">", 100).OrderByDesc("id").Limit(10).ToSQL()
		},
		squirr: func() (string, []any, error) {
			return sqPG.Select("id", "name").From("users").Where(sq.Gt{"votes": 100}).OrderBy("id DESC").Limit(10).ToSql()
		},
		goqu: func() (string, []any, error) {
			return goquPG.From("users").Prepared(true).Select("id", "name").Where(goqu.C("votes").Gt(100)).Order(goqu.C("id").Desc()).Limit(10).ToSQL()
		},
		sqlb: func() (string, []any, error) {
			sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
			sql, args := sb.Select("id", "name").From("users").Where(sb.GreaterThan("votes", 100)).OrderByDesc("id").Limit(10).Build()
			return sql, args, nil
		},
	},
	{
		name:      "ComplexSelect",
		limitArgs: 2,
		quill: func() (string, []any, error) {
			return pg.Table("users as u").
				Select("u.id", "u.name", "p.title").
				Join("posts as p", "p.user_id", "u.id").
				LeftJoin("profiles", "profiles.user_id", "=", "u.id").
				Where("u.age", ">=", 18).
				WhereIn("u.id", ids).
				WhereNull("u.deleted_at").
				GroupBy("u.id", "u.name", "p.title").
				Having("count(*)", ">", 1).
				OrderBy("u.name", "asc").
				Limit(50).Offset(100).
				ToSQL()
		},
		squirr: func() (string, []any, error) {
			return sqPG.Select("u.id", "u.name", "p.title").
				From("users AS u").
				Join("posts AS p ON p.user_id = u.id").
				LeftJoin("profiles ON profiles.user_id = u.id").
				Where(sq.GtOrEq{"u.age": 18}).
				Where(sq.Eq{"u.id": ids}).
				Where(sq.Eq{"u.deleted_at": nil}).
				GroupBy("u.id", "u.name", "p.title").
				Having("count(*) > ?", 1).
				OrderBy("u.name ASC").
				Limit(50).Offset(100).
				ToSql()
		},
		goqu: func() (string, []any, error) {
			return goquPG.From(goqu.T("users").As("u")).Prepared(true).
				Select(goqu.I("u.id"), goqu.I("u.name"), goqu.I("p.title")).
				Join(goqu.T("posts").As("p"), goqu.On(goqu.I("p.user_id").Eq(goqu.I("u.id")))).
				LeftJoin(goqu.T("profiles"), goqu.On(goqu.I("profiles.user_id").Eq(goqu.I("u.id")))).
				Where(goqu.I("u.age").Gte(18), goqu.I("u.id").In(ids), goqu.I("u.deleted_at").IsNull()).
				GroupBy(goqu.I("u.id"), goqu.I("u.name"), goqu.I("p.title")).
				Having(goqu.COUNT(goqu.Star()).Gt(1)).
				Order(goqu.I("u.name").Asc()).
				Limit(50).Offset(100).
				ToSQL()
		},
		sqlb: func() (string, []any, error) {
			sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
			sql, args := sb.Select("u.id", "u.name", "p.title").
				From("users AS u").
				Join("posts AS p", "p.user_id = u.id").
				JoinWithOption(sqlbuilder.LeftJoin, "profiles", "profiles.user_id = u.id").
				Where(sb.GreaterEqualThan("u.age", 18), sb.In("u.id", idsAny...), sb.IsNull("u.deleted_at")).
				GroupBy("u.id", "u.name", "p.title").
				Having(sb.GreaterThan("count(*)", 1)).
				OrderByAsc("u.name").
				Limit(50).Offset(100).
				Build()
			return sql, args, nil
		},
	},
	{
		name: "WideWhere",
		quill: func() (string, []any, error) {
			return pg.Table("orders").
				Where("a", 1).Where("b", ">", 2).Where("c", "<", 3).Where("d", "!=", 4).
				Where("e", ">=", 5).Where("f", "<=", 6).WhereIn("g", ids).WhereNull("h").
				WhereNotNull("i").WhereBetween("j", 7, 8).WhereLike("k", "x%").Where("l", 9).
				ToSQL()
		},
		squirr: func() (string, []any, error) {
			return sqPG.Select("*").From("orders").
				Where(sq.Eq{"a": 1}).Where(sq.Gt{"b": 2}).Where(sq.Lt{"c": 3}).Where(sq.NotEq{"d": 4}).
				Where(sq.GtOrEq{"e": 5}).Where(sq.LtOrEq{"f": 6}).Where(sq.Eq{"g": ids}).Where(sq.Eq{"h": nil}).
				Where(sq.NotEq{"i": nil}).Where("j BETWEEN ? AND ?", 7, 8).Where(sq.Like{"k": "x%"}).Where(sq.Eq{"l": 9}).
				ToSql()
		},
		goqu: func() (string, []any, error) {
			return goquPG.From("orders").Prepared(true).Where(
				goqu.C("a").Eq(1), goqu.C("b").Gt(2), goqu.C("c").Lt(3), goqu.C("d").Neq(4),
				goqu.C("e").Gte(5), goqu.C("f").Lte(6), goqu.C("g").In(ids), goqu.C("h").IsNull(),
				goqu.C("i").IsNotNull(), goqu.C("j").Between(goqu.Range(7, 8)), goqu.C("k").Like("x%"), goqu.C("l").Eq(9),
			).ToSQL()
		},
		sqlb: func() (string, []any, error) {
			sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
			sql, args := sb.Select("*").From("orders").Where(
				sb.Equal("a", 1), sb.GreaterThan("b", 2), sb.LessThan("c", 3), sb.NotEqual("d", 4),
				sb.GreaterEqualThan("e", 5), sb.LessEqualThan("f", 6), sb.In("g", idsAny...), sb.IsNull("h"),
				sb.IsNotNull("i"), sb.Between("j", 7, 8), sb.Like("k", "x%"), sb.Equal("l", 9),
			).Build()
			return sql, args, nil
		},
	},
	{
		name: "Insert3Rows",
		quill: func() (string, []any, error) {
			rows := make([]map[string]any, len(insRows))
			for i, r := range insRows {
				rows[i] = map[string]any{"name": r[0], "age": r[1], "email": r[2]}
			}
			return pg.Table("users").InsertSQL(rows...)
		},
		squirr: func() (string, []any, error) {
			q := sqPG.Insert("users").Columns("name", "age", "email")
			for _, r := range insRows {
				q = q.Values(r[0], r[1], r[2])
			}
			return q.ToSql()
		},
		goqu: func() (string, []any, error) {
			q := goquPG.Insert("users").Prepared(true).Cols("name", "age", "email")
			for _, r := range insRows {
				q = q.Vals(goqu.Vals{r[0], r[1], r[2]})
			}
			return q.ToSQL()
		},
		sqlb: func() (string, []any, error) {
			ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
			ib.InsertInto("users").Cols("name", "age", "email")
			for _, r := range insRows {
				ib.Values(r[0], r[1], r[2])
			}
			sql, args := ib.Build()
			return sql, args, nil
		},
	},
	{
		name: "Update",
		quill: func() (string, []any, error) {
			return pg.Table("users").Where("id", 1).UpdateSQL(map[string]any{"name": "ann", "age": 31})
		},
		squirr: func() (string, []any, error) {
			return sqPG.Update("users").Set("name", "ann").Set("age", 31).Where(sq.Eq{"id": 1}).ToSql()
		},
		goqu: func() (string, []any, error) {
			return goquPG.Update("users").Prepared(true).Set(goqu.Record{"name": "ann", "age": 31}).Where(goqu.C("id").Eq(1)).ToSQL()
		},
		sqlb: func() (string, []any, error) {
			ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
			sql, args := ub.Update("users").Set(ub.Assign("name", "ann"), ub.Assign("age", 31)).Where(ub.Equal("id", 1)).Build()
			return sql, args, nil
		},
	},
	{
		name: "Delete",
		quill: func() (string, []any, error) {
			return pg.Table("users").Where("id", 1).DeleteSQL()
		},
		squirr: func() (string, []any, error) {
			return sqPG.Delete("users").Where(sq.Eq{"id": 1}).ToSql()
		},
		goqu: func() (string, []any, error) {
			return goquPG.Delete("users").Prepared(true).Where(goqu.C("id").Eq(1)).ToSQL()
		},
		sqlb: func() (string, []any, error) {
			db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
			sql, args := db.DeleteFrom("users").Where(db.Equal("id", 1)).Build()
			return sql, args, nil
		},
	},
}

func libs(s scenario) []struct {
	name string
	fn   build
} {
	return []struct {
		name string
		fn   build
	}{{"quill", s.quill}, {"squirrel", s.squirr}, {"goqu", s.goqu}, {"sqlbuilder", s.sqlb}}
}

// TestScenariosAreComparable guards the comparison itself: every library must build
// the query without error and bind the same number of arguments, so a benchmark
// cannot "win" by quietly building a different query. Run with -v to see the SQL.
func TestScenariosAreComparable(t *testing.T) {
	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			want := -1
			for _, l := range libs(s) {
				sql, args, err := l.fn()
				require.NoError(t, err, l.name)
				require.NotEmpty(t, sql, l.name)
				t.Logf("%-8s %s  %v", l.name, sql, args)
				n := len(args)
				if l.name == "goqu" || l.name == "sqlbuilder" {
					n -= s.limitArgs
				}
				if want < 0 {
					want = n
				}
				assert.Equal(t, want, n, "%s binds a different number of arguments", l.name)
			}
		})
	}
}

func BenchmarkBuild(b *testing.B) {
	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			for _, l := range libs(s) {
				b.Run(l.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if _, _, err := l.fn(); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
