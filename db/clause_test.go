package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

// Clauses keep their rarely used parts (nested groups, subqueries, raw SQL) behind
// a pointer. A clone must not share that state with the original.
func TestCloneKeepsClauseExtensionsIndependent(t *testing.T) {
	for _, d := range []db.Dialect{db.Postgres, db.MySQL, db.SQLite, db.MariaDB} {
		t.Run(d.String(), func(t *testing.T) {
			sub := db.Use(d).Table("posts").Select("user_id").Where("views", ">", 10)
			base := db.Use(d).Table("users").
				WhereGroup(func(g *db.Builder) { g.Where("a", 1).OrWhere("b", 2) }).
				WhereExists(sub).
				WhereIn("id", sub).
				WhereRaw("x = ?", 7).
				WhereDate("created_at", "2024-01-01")

			before, beforeArgs, err := base.ToSQL()
			require.NoError(t, err)

			clone := base.Clone().Where("extra", 1)
			sub.Where("late", 99) // mutating the original subquery after it was added
			base.WhereGroup(func(g *db.Builder) { g.Where("c", 3) })

			cloneSQL, _, err := clone.ToSQL()
			require.NoError(t, err)
			assert.NotContains(t, cloneSQL, "late")
			assert.NotContains(t, cloneSQL, `"c"`)
			assert.NotContains(t, cloneSQL, "`c`")

			again, againArgs, err := db.Use(d).Table("users").
				WhereGroup(func(g *db.Builder) { g.Where("a", 1).OrWhere("b", 2) }).
				WhereExists(db.Use(d).Table("posts").Select("user_id").Where("views", ">", 10)).
				WhereIn("id", db.Use(d).Table("posts").Select("user_id").Where("views", ">", 10)).
				WhereRaw("x = ?", 7).
				WhereDate("created_at", "2024-01-01").
				ToSQL()
			require.NoError(t, err)
			assert.Equal(t, again, before)
			assert.Equal(t, againArgs, beforeArgs)
		})
	}
}

func TestNegatedExistsConnectives(t *testing.T) {
	sub := db.Use(db.Postgres).Table("p").Where("x", 1)
	sql, _, err := db.Use(db.Postgres).Table("u").WhereNotExists(sub).OrWhereNotExists(sub).Where("a", 1).OrWhereExists(sub).ToSQL()
	require.NoError(t, err)
	assert.Equal(t, `select * from "u" where not exists (select * from "p" where "x" = $1) or not exists (select * from "p" where "x" = $2) and "a" = $3 or exists (select * from "p" where "x" = $4)`, sql)
}
