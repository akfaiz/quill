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
		mysql, _, _ := db.Use(db.MySQL).Select("opts->theme").From("t").Where("opts->rank", ">", 1).ToSQL()
		maria, _, _ := db.Use(db.MariaDB).Select("opts->theme").From("t").Where("opts->rank", ">", 1).ToSQL()
		want := "SELECT JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"theme\"')) FROM `t` WHERE JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"rank\"')) > ?"
		if mysql != want || maria != want {
			t.Errorf("mysql:   %s\nmariadb: %s\nwant:    %s", mysql, maria, want)
		}
	})

	t.Run("JSON container values use json_query, not cast as json", func(t *testing.T) {
		set := map[string]any{"opts->tags": []string{"a"}, "opts->meta": map[string]any{"k": 1}}
		mysql, _, _ := db.Use(db.MySQL).Update("t").Where("id", 1).SetMap(set).ToSQL()
		maria, _, _ := db.Use(db.MariaDB).Update("t").Where("id", 1).SetMap(set).ToSQL()
		if want := "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"meta\"', CAST(? AS JSON), '$.\"tags\"', CAST(? AS JSON)) WHERE `id` = ?"; mysql != want {
			t.Errorf("mysql: %s", mysql)
		}
		if want := "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"meta\"', JSON_QUERY(?, '$'), '$.\"tags\"', JSON_QUERY(?, '$')) WHERE `id` = ?"; maria != want {
			t.Errorf("mariadb: %s", maria)
		}
		// scalar values are unaffected
		a, _, _ := db.Use(db.MySQL).Update("t").Where("id", 1).SetMap(map[string]any{"opts->n": 1}).ToSQL()
		b, _, _ := db.Use(db.MariaDB).Update("t").Where("id", 1).SetMap(map[string]any{"opts->n": 1}).ToSQL()
		assert.Equal(t, b, a)
	})

	t.Run("lateral joins are unsupported", func(t *testing.T) {
		sub := db.Use(db.MariaDB).Select().From("posts").Limit(1)
		for name, q := range map[string]*db.SelectBuilder{
			"JoinLateral":     db.Use(db.MariaDB).Select().From("users").JoinLateral(sub, "p"),
			"LeftJoinLateral": db.Use(db.MariaDB).Select().From("users").LeftJoinLateral(sub, "p"),
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
			return db.Use(d).Select("u.id").From("users as u").Where("age", ">", 3).WhereIn("id", []int{1, 2}).
				OrderBy("id").Limit(5).Offset(10).ToSQL()
		},
		"offset without limit": func(d db.Dialect) (string, []any, error) { return db.Use(d).Select().From("t").Skip(5).ToSQL() },
		"union": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Select().From("a").Union(db.Use(d).Select().From("b")).Limit(1).ToSQL()
		},
		"like and null-safe": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Select().From("t").WhereLike("a", "x%", true).WhereNullSafeEquals("b", nil).ToSQL()
		},
		"date parts": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Select().From("t").WhereDate("c", "2024-01-02").WhereMonth("c", 3).ToSQL()
		},
		"locks and hints": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Select().From("t").UseIndex("i").SharedLock().ToSQL()
		},
		"random": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Select().From("t").InRandomOrder(7).ToSQL()
		},
		"straight join": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Select().From("a").StraightJoin("b", "b.id", "a.id").ToSQL()
		},
		"JSON contains and keys": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).
				Select().From("t").
				WhereJsonContains("o->a", 1).
				WhereJsonContainsKey("o->b").
				WhereJsonLength("o->c", ">", 1).
				ToSQL()
		},
		"INSERT": func(d db.Dialect) (string, []any, error) { return db.Use(d).Insert("t").Values(rows...).ToSQL() },
		"INSERT IGNORE": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Insert("t").Values(rows...).Ignore().ToSQL()
		},
		"insert default": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Insert("t").Values(map[string]any{}).ToSQL()
		},
		"upsert": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Insert("t").Values(rows...).Upsert(nil, []string{"name"}).ToSQL()
		},
		"update": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Update("t").Where("id", 1).SetMap(map[string]any{"a": 1}).ToSQL()
		},
		"update with join": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).
				Update("u").
				Join("o", "o.u", "u.id").
				Where("o.n", 1).
				SetMap(map[string]any{"u.x": 1}).
				ToSQL()
		},
		"update with limit": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Update("t").Where("a", 1).OrderBy("id").Limit(2).SetMap(map[string]any{"b": 1}).ToSQL()
		},
		"increment": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Update("t").Where("id", 1).Increment("n", 2).ToSQL()
		},
		"delete": func(d db.Dialect) (string, []any, error) { return db.Use(d).Delete("t").Where("id", 1).ToSQL() },
		"delete with join": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Delete("p as a").Join("u as b", "b.id", "a.u").Where("b.n", 1).ToSQL()
		},
		"returning is unsupported": func(d db.Dialect) (string, []any, error) {
			return db.Use(d).Insert("t").Values(map[string]any{"a": 1}).Returning("id").ToSQL()
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
	my, _ := db.Use(db.MySQL).TruncateSQL("t")
	ma, _ := db.Use(db.MariaDB).TruncateSQL("t")
	if len(my) != 1 || len(ma) != 1 || my[0].SQL != ma[0].SQL {
		t.Errorf("truncate differs: %v vs %v", my, ma)
	}
}
