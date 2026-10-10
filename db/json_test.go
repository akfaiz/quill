package db_test

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

// want is the expected outcome for one dialect: either SQL+args or an error.
type want struct {
	sql  string
	args []any
	err  error
}

type perCase struct {
	name         string
	build        func(d db.Dialect) (string, []any, error)
	pg, my, lite want
	maria        want // zero value: same as my
}

func selectOf(f func(d db.Dialect) *db.Builder) func(db.Dialect) (string, []any, error) {
	return func(d db.Dialect) (string, []any, error) { return f(d).ToSQL() }
}

// mariaWant is the MariaDB expectation: the explicit override, else MySQL's.
func mariaWant(tc perCase) want {
	if reflect.DeepEqual(tc.maria, want{}) { //nolint:govet // want holds an error and slices; no cheaper comparison.
		return tc.my
	}
	return tc.maria
}

func runPer(t *testing.T, cases []perCase) {
	t.Helper()
	for _, tc := range cases {
		for _, dl := range []struct {
			d db.Dialect
			w want
		}{{db.Postgres, tc.pg}, {db.MySQL, tc.my}, {db.SQLite, tc.lite}, {db.MariaDB, mariaWant(tc)}} {
			t.Run(tc.name+"/"+dl.d.String(), func(t *testing.T) {
				sql, args, err := tc.build(dl.d)
				if dl.w.err != nil {
					require.ErrorIs(t, err, dl.w.err, "sql=%q", sql)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, dl.w.sql, sql)
				if len(args) == 0 && len(dl.w.args) == 0 {
					return
				}
				assert.Equal(t, dl.w.args, args)
			})
		}
	}
}

func jt(d db.Dialect) *db.Builder { return db.Use(d).Table("t") }

func TestJSONSelectorsAndWheres(t *testing.T) {
	runPer(t, []perCase{
		{
			name: "selectors in select list with alias and array index",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).Select("options->theme", "opts->a->b as v", "u.meta->x[0]->y")
			}),
			pg: want{sql: `SELECT "options"->>'theme', "opts"->'a'->>'b' AS "v", "u"."meta"->'x'->0->>'y' FROM "t"`},
			my: want{
				sql: "SELECT JSON_UNQUOTE(JSON_EXTRACT(`options`, '$.\"theme\"')), JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"a\".\"b\"')) AS `v`, JSON_UNQUOTE(JSON_EXTRACT(`u`.`meta`, '$.\"x\"[0].\"y\"')) FROM `t`",
			},
			lite: want{
				sql: `SELECT JSON_EXTRACT("options", '$."theme"'), JSON_EXTRACT("opts", '$."a"."b"') AS "v", JSON_EXTRACT("u"."meta", '$."x"[0]."y"') FROM "t"`,
			},
		},
		{
			name: "text comparison on selector",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).Where("opts->theme", "dark").OrderBy("opts->rank")
			}),
			pg: want{
				sql:  `SELECT * FROM "t" WHERE "opts"->>'theme' = $1 ORDER BY "opts"->>'rank' ASC`,
				args: []any{"dark"},
			},
			my: want{
				sql:  "SELECT * FROM `t` WHERE JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"theme\"')) = ? ORDER BY JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"rank\"')) ASC",
				args: []any{"dark"},
			},
			lite: want{
				sql:  `SELECT * FROM "t" WHERE JSON_EXTRACT("opts", '$."theme"') = ? ORDER BY JSON_EXTRACT("opts", '$."rank"') ASC`,
				args: []any{"dark"},
			},
		},
		{
			name: "boolean comparison compares as json",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).Where("opts->on", true).OrWhere("opts->off", "!=", false)
			}),
			pg: want{
				sql: `SELECT * FROM "t" WHERE ("opts"->'on')::JSONB = 'true'::JSONB OR ("opts"->'off')::JSONB != 'false'::JSONB`,
			},
			my: want{
				sql: "SELECT * FROM `t` WHERE JSON_EXTRACT(`opts`, '$.\"on\"') = TRUE OR JSON_EXTRACT(`opts`, '$.\"off\"') != FALSE",
			},
			lite: want{
				sql: `SELECT * FROM "t" WHERE JSON_EXTRACT("opts", '$."on"') = TRUE OR JSON_EXTRACT("opts", '$."off"') != FALSE`,
			},
		},
		{
			name: "contains scalar and not contains list",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(
					d,
				).WhereJsonContains("opts->tags", "go").
					OrWhereJsonDoesntContain("opts->tags", []string{"a", "b"})
			}),
			pg: want{
				sql:  `SELECT * FROM "t" WHERE ("opts"->'tags')::JSONB @> $1 OR NOT ("opts"->'tags')::JSONB @> $2`,
				args: []any{`"go"`, `["a","b"]`},
			},
			my: want{
				sql:  "SELECT * FROM `t` WHERE JSON_CONTAINS(`opts`, ?, '$.\"tags\"') OR NOT JSON_CONTAINS(`opts`, ?, '$.\"tags\"')",
				args: []any{`"go"`, `["a","b"]`},
			},
			lite: want{
				sql:  `SELECT * FROM "t" WHERE EXISTS (SELECT 1 FROM JSON_EACH("opts", '$."tags"') WHERE "json_each"."value" IS ?) OR NOT EXISTS (SELECT 1 FROM JSON_EACH("opts", '$."tags"') WHERE "json_each"."value" IS ?)`,
				args: []any{"go", []string{"a", "b"}},
			},
		},
		{
			name: "contains encodes html chars unescaped",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonContains("opts", map[string]any{"q": "a<b&c"})
			}),
			pg: want{sql: `SELECT * FROM "t" WHERE ("opts")::JSONB @> $1`, args: []any{`{"q":"a<b&c"}`}},
			my: want{sql: "SELECT * FROM `t` WHERE JSON_CONTAINS(`opts`, ?)", args: []any{`{"q":"a<b&c"}`}},
			lite: want{
				sql:  `SELECT * FROM "t" WHERE EXISTS (SELECT 1 FROM JSON_EACH("opts") WHERE "json_each"."value" IS ?)`,
				args: []any{map[string]any{"q": "a<b&c"}},
			},
		},
		{
			name: "contains key",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonContainsKey("opts->k").OrWhereJsonDoesntContainKey("opts->a->b")
			}),
			pg: want{
				sql: `SELECT * FROM "t" WHERE COALESCE(("opts")::JSONB ? 'k', FALSE) OR NOT COALESCE(("opts"->'a')::JSONB ? 'b', FALSE)`,
			},
			my: want{
				sql: "SELECT * FROM `t` WHERE IFNULL(JSON_CONTAINS_PATH(`opts`, 'one', '$.\"k\"'), 0) OR NOT IFNULL(JSON_CONTAINS_PATH(`opts`, 'one', '$.\"a\".\"b\"'), 0)",
			},
			lite: want{
				sql: `SELECT * FROM "t" WHERE JSON_TYPE("opts", '$."k"') IS NOT NULL OR NOT JSON_TYPE("opts", '$."a"."b"') IS NOT NULL`,
			},
		},
		{
			name: "contains key with array index on postgres",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonContainsKey("opts->tags->2").WhereJsonContainsKey("opts->tags[-2]")
			}),
			pg: want{
				sql: `SELECT * FROM "t" WHERE CASE WHEN JSONB_TYPEOF(("opts"->'tags')::JSONB) = 'array' THEN JSONB_ARRAY_LENGTH(("opts"->'tags')::JSONB) >= 3 ELSE FALSE END AND CASE WHEN JSONB_TYPEOF(("opts"->'tags')::JSONB) = 'array' THEN JSONB_ARRAY_LENGTH(("opts"->'tags')::JSONB) >= 2 ELSE FALSE END`,
			},
			my: want{
				sql: "SELECT * FROM `t` WHERE IFNULL(JSON_CONTAINS_PATH(`opts`, 'one', '$.\"tags\".\"2\"'), 0) AND IFNULL(JSON_CONTAINS_PATH(`opts`, 'one', '$.\"tags\"[-2]'), 0)",
			},
			lite: want{
				sql: `SELECT * FROM "t" WHERE JSON_TYPE("opts", '$."tags"."2"') IS NOT NULL AND JSON_TYPE("opts", '$."tags"[-2]') IS NOT NULL`,
			},
		},
		{
			name: "length",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonLength("opts->tags", ">", 2).OrWhereJsonLength("opts->tags", "3")
			}),
			pg: want{
				sql:  `SELECT * FROM "t" WHERE JSONB_ARRAY_LENGTH(("opts"->'tags')::JSONB) > $1 OR JSONB_ARRAY_LENGTH(("opts"->'tags')::JSONB) = $2`,
				args: []any{int64(2), int64(3)},
			},
			my: want{
				sql:  "SELECT * FROM `t` WHERE JSON_LENGTH(`opts`, '$.\"tags\"') > ? OR JSON_LENGTH(`opts`, '$.\"tags\"') = ?",
				args: []any{int64(2), int64(3)},
			},
			lite: want{
				sql:  `SELECT * FROM "t" WHERE JSON_ARRAY_LENGTH("opts", '$."tags"') > ? OR JSON_ARRAY_LENGTH("opts", '$."tags"') = ?`,
				args: []any{int64(2), int64(3)},
			},
		},
		{
			name: "overlaps mysql only",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonOverlaps("opts", []int{1, 2}).OrWhereJsonDoesntOverlap("opts->ids", []int{3})
			}),
			pg: want{err: db.ErrUnsupported},
			my: want{
				sql:  "SELECT * FROM `t` WHERE JSON_OVERLAPS(`opts`, ?) OR NOT JSON_OVERLAPS(JSON_EXTRACT(`opts`, '$.\"ids\"'), ?)",
				args: []any{"[1,2]", "[3]"},
			},
			lite: want{err: db.ErrUnsupported},
		},
		{
			name: "single quote in path cannot break out of the literal",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).Select(`opts->a\'b`)
			}),
			pg:   want{sql: `SELECT "opts"->>'a\''b' FROM "t"`},
			my:   want{sql: "SELECT JSON_UNQUOTE(JSON_EXTRACT(`opts`, '$.\"a''b\"')) FROM `t`"},
			lite: want{sql: `SELECT JSON_EXTRACT("opts", '$."a''b"') FROM "t"`},
		},
	})
}

func TestJSONUpdates(t *testing.T) {
	runPer(t, []perCase{
		{
			name: "single path with plain column",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("opts->theme", "dark", "name", "x"))
			},
			pg: want{
				sql:  `UPDATE "t" SET "name" = $1, "opts" = JSONB_SET("opts"::JSONB, '{"theme"}', $2) WHERE "id" = $3`,
				args: []any{"x", `"dark"`, 1},
			},
			my: want{
				sql:  "UPDATE `t` SET `name` = ?, `opts` = JSON_SET(`opts`, '$.\"theme\"', ?) WHERE `id` = ?",
				args: []any{"x", "dark", 1},
			},
			lite: want{
				sql:  `UPDATE "t" SET "name" = ?, "opts" = JSON_PATCH(IFNULL("opts", JSON('{}')), JSON(?)) WHERE "id" = ?`,
				args: []any{"x", `{"theme":"dark"}`, 1},
			},
		},
		{
			name: "bool and container values",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("opts->on", true, "opts->tags", []string{"a"}))
			},
			pg: want{
				sql:  `UPDATE "t" SET "opts" = JSONB_SET(JSONB_SET("opts"::JSONB, '{"on"}', $1), '{"tags"}', $2) WHERE "id" = $3`,
				args: []any{"true", `["a"]`, 1},
			},
			my: want{
				sql:  "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"on\"', TRUE, '$.\"tags\"', CAST(? AS JSON)) WHERE `id` = ?",
				args: []any{`["a"]`, 1},
			},
			lite: want{
				sql:  `UPDATE "t" SET "opts" = JSON_PATCH(IFNULL("opts", JSON('{}')), JSON(?)) WHERE "id" = ?`,
				args: []any{`{"on":true,"tags":["a"]}`, 1},
			},
			maria: want{
				sql:  "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"on\"', TRUE, '$.\"tags\"', JSON_QUERY(?, '$')) WHERE `id` = ?",
				args: []any{`["a"]`, 1},
			},
		},
		{
			name: "nested paths and array index",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("opts->a->b", 1, "opts->a->c", 2, "opts->d", 3))
			},
			pg: want{
				sql:  `UPDATE "t" SET "opts" = JSONB_SET(JSONB_SET(JSONB_SET("opts"::JSONB, '{"a","b"}', $1), '{"a","c"}', $2), '{"d"}', $3) WHERE "id" = $4`,
				args: []any{"1", "2", "3", 1},
			},
			my: want{
				sql:  "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"a\".\"b\"', ?, '$.\"a\".\"c\"', ?, '$.\"d\"', ?) WHERE `id` = ?",
				args: []any{1, 2, 3, 1},
			},
			lite: want{
				sql:  `UPDATE "t" SET "opts" = JSON_PATCH(IFNULL("opts", JSON('{}')), JSON(?)) WHERE "id" = ?`,
				args: []any{`{"a":{"b":1,"c":2},"d":3}`, 1},
			},
		},
		{
			name: "array index path and qualified column",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("t.opts->tags[0]", "x"))
			},
			pg: want{
				sql:  `UPDATE "t" SET "opts" = JSONB_SET("opts"::JSONB, '{"tags",0}', $1) WHERE "id" = $2`,
				args: []any{`"x"`, 1},
			},
			my: want{
				sql:  "UPDATE `t` SET `t`.`opts` = JSON_SET(`t`.`opts`, '$.\"tags\"[0]', ?) WHERE `id` = ?",
				args: []any{"x", 1},
			},
			lite: want{
				sql:  `UPDATE "t" SET "opts" = JSON_PATCH(IFNULL("opts", JSON('{}')), JSON(?)) WHERE "id" = ?`,
				args: []any{`{"tags[0]":"x"}`, 1},
			},
		},
		{
			name: "raw expression value",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("opts->n", db.Raw("1 + 1")))
			},
			pg: want{
				sql:  `UPDATE "t" SET "opts" = JSONB_SET("opts"::JSONB, '{"n"}', 1 + 1) WHERE "id" = $1`,
				args: []any{1},
			},
			my: want{
				sql:  "UPDATE `t` SET `opts` = JSON_SET(`opts`, '$.\"n\"', 1 + 1) WHERE `id` = ?",
				args: []any{1},
			},
			lite: want{err: db.ErrUnsupported},
		},
	})
}

func TestJSONErrors(t *testing.T) {
	d := db.Postgres
	tests := []struct {
		name  string
		build *db.Builder
		want  error
	}{
		{"length not integer", jt(d).WhereJsonLength("opts->a", "many"), db.ErrInvalidArgs},
		{"length nil", jt(d).WhereJsonLength("opts->a", "=", nil), db.ErrInvalidArgs},
		{"postgres contains key needs path", jt(d).WhereJsonContainsKey("opts"), db.ErrInvalidArgs},
		{"unencodable value", jt(d).WhereJsonContains("opts", make(chan int)), db.ErrInvalidArgs},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.build.ToSQL()
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestJSONNegatedAndOrVariantsPostgres(t *testing.T) {
	sql, args, err := db.Use(db.Postgres).Table("t").
		OrWhereJsonContains("o->a", 1).
		WhereJsonDoesntContainKey("o->b").
		OrWhereJsonContainsKey("o->c").
		ToSQL()
	require.NoError(t, err)
	want := `SELECT * FROM "t" WHERE ("o"->'a')::JSONB @> $1 AND NOT COALESCE(("o")::JSONB ? 'b', FALSE) OR COALESCE(("o")::JSONB ? 'c', FALSE)`
	if sql != want || !reflect.DeepEqual(args, []any{"1"}) {
		t.Errorf("got %s %v", sql, args)
	}
}

func TestJSONOverlapVariantsMySQL(t *testing.T) {
	sql, args, err := db.Use(db.MySQL).Table("t").
		OrWhereJsonOverlaps("o", []int{1}).
		WhereJsonDoesntOverlap("o->x", []int{2}).
		OrWhereJsonDoesntOverlap("o->y", []int{3}).
		ToSQL()
	require.NoError(t, err)
	want := "SELECT * FROM `t` WHERE JSON_OVERLAPS(`o`, ?) AND NOT JSON_OVERLAPS(JSON_EXTRACT(`o`, '$.\"x\"'), ?) OR NOT JSON_OVERLAPS(JSON_EXTRACT(`o`, '$.\"y\"'), ?)"
	if sql != want || !reflect.DeepEqual(args, []any{"[1]", "[2]", "[3]"}) {
		t.Errorf("got %s %v", sql, args)
	}
}

func TestWhereJsonDoesntContainNegatesContains(t *testing.T) {
	sqlText, args, err := jt(db.Postgres).WhereJsonDoesntContain("options->tags", "php").ToSQL()
	require.NoError(t, err)
	assert.Equal(t, `SELECT * FROM "t" WHERE NOT ("options"->'tags')::JSONB @> $1`, sqlText)
	assert.Equal(t, []any{`"php"`}, args)
}
