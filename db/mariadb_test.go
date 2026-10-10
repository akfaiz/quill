package db_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/akfaiz/quill/db"
)

func TestMariaDBName(t *testing.T) {
	if got := db.MariaDB.String(); got != "mariadb" {
		t.Errorf("String() = %q", got)
	}
}

// The places where MariaDB's SQL differs from MySQL's: two, both rejected by the server otherwise.
func TestMariaDBDiffersFromMySQLInTwoPlaces(t *testing.T) {
	t.Run("JSON text selectors stay json_unquote(json_extract), not json_value", func(t *testing.T) {
		// json_value returns NULL for arrays and objects on MariaDB; json_extract keeps the JSON text
		mysql, _, _ := db.Use(db.MySQL).Table("t").Select("opts->theme").Where("opts->rank", ">", 1).ToSQL()
		maria, _, _ := db.Use(db.MariaDB).Table("t").Select("opts->theme").Where("opts->rank", ">", 1).ToSQL()
		want := "SELECT JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"theme\"')) FROM `t` WHERE JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"rank\"')) > ?"
		if mysql != want || maria != want {
			t.Errorf("mysql:   %s\nmariadb: %s\nwant:    %s", mysql, maria, want)
		}
	})

	t.Run("JSON container values use json_query, not cast as json", func(t *testing.T) {
		set := map[string]any{"opts->tags": []string{"a"}, "opts->meta": map[string]any{"k": 1}}
		mysql, _, _ := db.Use(db.MySQL).Table("t").Where("id", 1).UpdateSQL(set)
		maria, _, _ := db.Use(db.MariaDB).Table("t").Where("id", 1).UpdateSQL(set)
		if want := "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"meta\"', CAST(? AS JSON), '$.\"tags\"', CAST(? AS JSON)) WHERE `id` = ?"; mysql != want {
			t.Errorf("mysql: %s", mysql)
		}
		if want := "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"meta\"', JSON_QUERY(?, '$'), '$.\"tags\"', JSON_QUERY(?, '$')) WHERE `id` = ?"; maria != want {
			t.Errorf("mariadb: %s", maria)
		}
		// scalar values are unaffected
		a, _, _ := db.Use(db.MySQL).Table("t").Where("id", 1).UpdateSQL(map[string]any{"opts->n": 1})
		b, _, _ := db.Use(db.MariaDB).Table("t").Where("id", 1).UpdateSQL(map[string]any{"opts->n": 1})
		assert.Equal(t, b, a)
	})

	t.Run("lateral joins are unsupported", func(t *testing.T) {
		sub := db.Use(db.MariaDB).Table("posts").Limit(1)
		for name, q := range map[string]*db.Builder{
			"JoinLateral":     db.Use(db.MariaDB).Table("users").JoinLateral(sub, "p"),
			"LeftJoinLateral": db.Use(db.MariaDB).Table("users").LeftJoinLateral(sub, "p"),
		} {
			if _, _, err := q.ToSQL(); !errors.Is(err, db.ErrUnsupported) {
				t.Errorf("%s: got %v, want ErrUnsupported", name, err)
			}
		}
	})
}

// Everything else is MySQL's. This compares the two dialects directly, so a change
// to the MySQL grammar cannot silently leave MariaDB behind.
func TestMariaDBMatchesMySQLEverywhereElse(t *testing.T) {
	rows := []map[string]any{{"email": "a@x", "name": "A"}, {"email": "b@x", "name": "B"}}
	builds := map[string]func(d db.Dialect) (string, []any, error){
		"select": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("users as u").Select("u.id").Where("age", ">", 3).WhereIn("id", []int{1, 2}).
				OrderBy("id").Limit(5).Offset(10).ToSQL()
		},
		"offset without limit": func(d db.Dialect) (string, []any, error) { return db.Use(d).Table("t").Skip(5).ToSQL() },
		"union": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("a").Union(db.Use(d).Table("b")).Limit(1).ToSQL()
		},
		"like and null-safe": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").WhereLike("a", "x%", true).WhereNullSafeEquals("b", nil).ToSQL()
		},
		"date parts": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").WhereDate("c", "2024-01-02").WhereMonth("c", 3).ToSQL()
		},
		"locks and hints": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").UseIndex("i").SharedLock().ToSQL()
		},
		"random": func(d db.Dialect) (string, []any, error) { return db.Use(d).Table("t").InRandomOrder(7).ToSQL() },
		"straight join": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("a").StraightJoin("b", "b.id", "a.id").ToSQL()
		},
		"JSON contains and keys": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).
				Table("t").
				WhereJsonContains("o->a", 1).
				WhereJsonContainsKey("o->b").
				WhereJsonLength("o->c", ">", 1).
				ToSQL()
		},
		"INSERT":         func(d db.Dialect) (string, []any, error) { return db.Use(d).Table("t").InsertSQL(rows...) },
		"INSERT IGNORE":  func(d db.Dialect) (string, []any, error) { return db.Use(d).Table("t").InsertOrIgnoreSQL(rows...) },
		"insert default": func(d db.Dialect) (string, []any, error) { return db.Use(d).Table("t").InsertSQL(map[string]any{}) },
		"upsert": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").UpsertSQL(rows, nil, []string{"name"})
		},
		"update": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").Where("id", 1).UpdateSQL(map[string]any{"a": 1})
		},
		"update with join": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("u").Join("o", "o.u", "u.id").Where("o.n", 1).UpdateSQL(map[string]any{"u.x": 1})
		},
		"update with limit": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").Where("a", 1).OrderBy("id").Limit(2).UpdateSQL(map[string]any{"b": 1})
		},
		"increment": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").Where("id", 1).IncrementSQL("n", 2, nil)
		},
		"delete": func(d db.Dialect) (string, []any, error) { return db.Use(d).Table("t").Where("id", 1).DeleteSQL() },
		"delete with join": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("p as a").Join("u as b", "b.id", "a.u").Where("b.n", 1).DeleteSQL()
		},
		"returning is unsupported": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Table("t").Returning("id").InsertSQL(map[string]any{"a": 1})
		},
	}
	for name, build := range builds {
		t.Run(name, func(t *testing.T) {
			mysqlSQL, mysqlArgs, mysqlErr := build(db.MySQL)
			mariaSQL, mariaArgs, mariaErr := build(db.MariaDB)
			if (mysqlErr == nil) != (mariaErr == nil) || (mysqlErr != nil && mysqlErr.Error() != mariaErr.Error()) {
				t.Fatalf("errors differ: mysql %v, mariadb %v", mysqlErr, mariaErr)
			}
			if mysqlSQL != mariaSQL || len(mysqlArgs) != len(mariaArgs) {
				t.Errorf("differs:\n mysql   %s %v\n mariadb %s %v", mysqlSQL, mysqlArgs, mariaSQL, mariaArgs)
			}
		})
	}

	// truncate has its own statement list
	my, _ := db.Use(db.MySQL).Table("t").TruncateSQL()
	ma, _ := db.Use(db.MariaDB).Table("t").TruncateSQL()
	if len(my) != 1 || len(ma) != 1 || my[0].SQL != ma[0].SQL {
		t.Errorf("truncate differs: %v vs %v", my, ma)
	}
}
