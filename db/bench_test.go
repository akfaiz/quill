package db_test

import (
	"testing"

	"github.com/akfaiz/quill/db"
)

var benchDialects = []db.Dialect{db.Postgres, db.MySQL, db.SQLite, db.MariaDB}

// benchSQL runs build + compile for every dialect as its own sub-benchmark.
func benchSQL(b *testing.B, build func(d db.Dialect) (string, []any, error)) {
	for _, d := range benchDialects {
		b.Run(d.String(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := build(d); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSelectSimple(b *testing.B) {
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		return db.Use(d).Table("users").Select("id", "name").Where("votes", ">", 100).OrderByDesc("id").Limit(10).ToSQL()
	})
}

func BenchmarkSelectComplex(b *testing.B) {
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		return db.Use(d).Table("users as u").
			Select("u.id", "u.name", "p.title").
			Join("posts as p", "p.user_id", "u.id").
			LeftJoin("profiles", "profiles.user_id", "=", "u.id").
			Where("u.active", true).
			WhereIn("u.id", []int{1, 2, 3, 4, 5}).
			WhereNull("u.deleted_at").
			GroupBy("u.id", "u.name", "p.title").
			Having("count(*)", ">", 1).
			OrderBy("u.name", "asc").
			Limit(50).Offset(100).
			ToSQL()
	})
}

func BenchmarkSelectSubquery(b *testing.B) {
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		sub := db.Use(d).Table("posts").Select("user_id").Where("views", ">", 10)
		return db.Use(d).Table("users").WhereIn("id", sub).Where("age", ">=", 18).ToSQL()
	})
}

func BenchmarkSelectCTE(b *testing.B) {
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		active := db.Use(d).Table("users").Where("active", true)
		return db.Use(d).Table("au").With("au", active).Where("id", ">", 5).ToSQL()
	})
}

func BenchmarkInsertSQL(b *testing.B) {
	rows := []map[string]any{
		{"name": "ann", "age": 30, "email": "a@x"},
		{"name": "bob", "age": 25, "email": "b@x"},
		{"name": "cy", "age": 41, "email": "c@x"},
	}
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		return db.Use(d).Table("users").InsertSQL(rows...)
	})
}

func BenchmarkUpdateSQL(b *testing.B) {
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		return db.Use(d).Table("users").Where("id", 1).UpdateSQL(map[string]any{"name": "ann", "age": 31})
	})
}

func BenchmarkUpsertSQL(b *testing.B) {
	rows := []map[string]any{{"email": "a@x", "name": "ann"}, {"email": "b@x", "name": "bob"}}
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		return db.Use(d).Table("users").UpsertSQL(rows, []string{"email"}, []string{"name"})
	})
}

func BenchmarkDeleteSQL(b *testing.B) {
	benchSQL(b, func(d db.Dialect) (string, []any, error) {
		return db.Use(d).Table("users").Where("id", 1).DeleteSQL()
	})
}

func BenchmarkToRawSQL(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if _, err := db.Use(db.Postgres).Table("users").Where("id", 1).Where("name", "ann").ToRawSQL(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkClone(b *testing.B) {
	base := db.Use(db.Postgres).Table("users").Where("a", 1).Join("posts", "posts.user_id", "users.id").OrderBy("id", "asc")
	b.ReportAllocs()
	for b.Loop() {
		_ = base.Clone().Where("b", 2)
	}
}

func BenchmarkRowFromStruct(b *testing.B) {
	type member struct {
		ID    int64  `db:"id,omitempty"`
		Name  string `db:"name"`
		Email string `db:"email,omitempty"`
	}
	m := member{Name: "ann", Email: "a@x"}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := db.Row(m); err != nil {
			b.Fatal(err)
		}
	}
}

// wideQuery is a realistic report-style query: many predicates, several joins,
// a nested group, a CTE and a union. It shows how cost scales with query size.
func wideQuery(d db.Dialect) *db.Builder {
	recent := db.Use(d).Table("orders").Select("user_id").Where("created_at", ">", "2024-01-01")
	q := db.Use(d).Table("users as u").
		With("recent", recent).
		Select("u.id", "u.name", "u.email", "p.title", "t.name as team", "c.name as company").
		Join("posts as p", "p.user_id", "u.id").
		LeftJoin("profiles as pr", "pr.user_id", "=", "u.id").
		LeftJoin("teams as t", "t.id", "=", "u.team_id").
		LeftJoin("companies as c", "c.id", "=", "t.company_id").
		LeftJoin("regions as r", "r.id", "=", "c.region_id").
		RightJoin("roles as ro", "ro.id", "=", "u.role_id").
		Where("u.active", true).
		Where("u.age", ">=", 18).
		Where("u.age", "<", 65).
		Where("u.score", ">", 1.5).
		WhereIn("u.id", recent).
		WhereIn("u.status", []string{"a", "b", "c"}).
		WhereNull("u.deleted_at").
		WhereNotNull("u.email").
		WhereBetween("u.created_at", "2023-01-01", "2023-12-31").
		WhereLike("u.name", "a%").
		WhereGroup(func(g *db.Builder) {
			g.Where("p.views", ">", 10).OrWhere("p.featured", true).OrWhereNull("p.archived_at")
		}).
		WhereColumn("u.team_id", "t.id").
		GroupBy("u.id", "u.name", "u.email", "p.title", "t.name", "c.name").
		Having("count(*)", ">", 1).
		OrderBy("u.name", "asc").
		OrderByDesc("u.id").
		Limit(50).Offset(100)
	return q.UnionAll(db.Use(d).Table("archived_users as u").Select("u.id", "u.name", "u.email", "u.title", "u.team", "u.company").Where("u.active", false))
}

func BenchmarkSelectWide(b *testing.B) {
	benchSQL(b, func(d db.Dialect) (string, []any, error) { return wideQuery(d).ToSQL() })
}

// BenchmarkSelectWideBuild measures only assembling the builder (clause storage).
func BenchmarkSelectWideBuild(b *testing.B) {
	for _, d := range benchDialects {
		b.Run(d.String(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = wideQuery(d)
			}
		})
	}
}

// BenchmarkSelectWideCompile measures only ToSQL on a builder built once.
func BenchmarkSelectWideCompile(b *testing.B) {
	for _, d := range benchDialects {
		q := wideQuery(d)
		b.Run(d.String(), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := q.ToSQL(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCloneWide(b *testing.B) {
	q := wideQuery(db.Postgres)
	b.ReportAllocs()
	for b.Loop() {
		_ = q.Clone()
	}
}

// BenchmarkJoinForms compares the plain join form with the closure form.
func BenchmarkJoinForms(b *testing.B) {
	b.Run("plain", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _, _ = db.Use(db.Postgres).Table("a").Join("b", "b.a_id", "a.id").Join("c", "c.a_id", "=", "a.id").ToSQL()
		}
	})
	b.Run("closure", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_, _, _ = db.Use(db.Postgres).Table("a").
				JoinFunc("b", func(j *db.JoinClause) { j.On("b.a_id", "a.id") }).
				JoinFunc("c", func(j *db.JoinClause) { j.On("c.a_id", "=", "a.id") }).ToSQL()
		}
	})
}
