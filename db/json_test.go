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
	if reflect.DeepEqual(tc.maria, want{}) {
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
			pg:   want{sql: `select "options"->>'theme', "opts"->'a'->>'b' as "v", "u"."meta"->'x'->0->>'y' from "t"`},
			my:   want{sql: "select json_unquote(json_extract(`options`, '$.\"theme\"')), json_unquote(json_extract(`opts`, '$.\"a\".\"b\"')) as `v`, json_unquote(json_extract(`u`.`meta`, '$.\"x\"[0].\"y\"')) from `t`"},
			lite: want{sql: `select json_extract("options", '$."theme"'), json_extract("opts", '$."a"."b"') as "v", json_extract("u"."meta", '$."x"[0]."y"') from "t"`},
		},
		{
			name: "text comparison on selector",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).Where("opts->theme", "dark").OrderBy("opts->rank")
			}),
			pg:   want{sql: `select * from "t" where "opts"->>'theme' = $1 order by "opts"->>'rank' asc`, args: []any{"dark"}},
			my:   want{sql: "select * from `t` where json_unquote(json_extract(`opts`, '$.\"theme\"')) = ? order by json_unquote(json_extract(`opts`, '$.\"rank\"')) asc", args: []any{"dark"}},
			lite: want{sql: `select * from "t" where json_extract("opts", '$."theme"') = ? order by json_extract("opts", '$."rank"') asc`, args: []any{"dark"}},
		},
		{
			name: "boolean comparison compares as json",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).Where("opts->on", true).OrWhere("opts->off", "!=", false)
			}),
			pg:   want{sql: `select * from "t" where ("opts"->'on')::jsonb = 'true'::jsonb or ("opts"->'off')::jsonb != 'false'::jsonb`},
			my:   want{sql: "select * from `t` where json_extract(`opts`, '$.\"on\"') = true or json_extract(`opts`, '$.\"off\"') != false"},
			lite: want{sql: `select * from "t" where json_extract("opts", '$."on"') = true or json_extract("opts", '$."off"') != false`},
		},
		{
			name: "contains scalar and not contains list",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonContains("opts->tags", "go").OrWhereJsonDoesntContain("opts->tags", []string{"a", "b"})
			}),
			pg:   want{sql: `select * from "t" where ("opts"->'tags')::jsonb @> $1 or not ("opts"->'tags')::jsonb @> $2`, args: []any{`"go"`, `["a","b"]`}},
			my:   want{sql: "select * from `t` where json_contains(`opts`, ?, '$.\"tags\"') or not json_contains(`opts`, ?, '$.\"tags\"')", args: []any{`"go"`, `["a","b"]`}},
			lite: want{sql: `select * from "t" where exists (select 1 from json_each("opts", '$."tags"') where "json_each"."value" is ?) or not exists (select 1 from json_each("opts", '$."tags"') where "json_each"."value" is ?)`, args: []any{"go", []string{"a", "b"}}},
		},
		{
			name: "contains encodes html chars unescaped",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonContains("opts", map[string]any{"q": "a<b&c"})
			}),
			pg:   want{sql: `select * from "t" where ("opts")::jsonb @> $1`, args: []any{`{"q":"a<b&c"}`}},
			my:   want{sql: "select * from `t` where json_contains(`opts`, ?)", args: []any{`{"q":"a<b&c"}`}},
			lite: want{sql: `select * from "t" where exists (select 1 from json_each("opts") where "json_each"."value" is ?)`, args: []any{map[string]any{"q": "a<b&c"}}},
		},
		{
			name: "contains key",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonContainsKey("opts->k").OrWhereJsonDoesntContainKey("opts->a->b")
			}),
			pg:   want{sql: `select * from "t" where coalesce(("opts")::jsonb ? 'k', false) or not coalesce(("opts"->'a')::jsonb ? 'b', false)`},
			my:   want{sql: "select * from `t` where ifnull(json_contains_path(`opts`, 'one', '$.\"k\"'), 0) or not ifnull(json_contains_path(`opts`, 'one', '$.\"a\".\"b\"'), 0)"},
			lite: want{sql: `select * from "t" where json_type("opts", '$."k"') is not null or not json_type("opts", '$."a"."b"') is not null`},
		},
		{
			name: "contains key with array index on postgres",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonContainsKey("opts->tags->2").WhereJsonContainsKey("opts->tags[-2]")
			}),
			pg:   want{sql: `select * from "t" where case when jsonb_typeof(("opts"->'tags')::jsonb) = 'array' then jsonb_array_length(("opts"->'tags')::jsonb) >= 3 else false end and case when jsonb_typeof(("opts"->'tags')::jsonb) = 'array' then jsonb_array_length(("opts"->'tags')::jsonb) >= 2 else false end`},
			my:   want{sql: "select * from `t` where ifnull(json_contains_path(`opts`, 'one', '$.\"tags\".\"2\"'), 0) and ifnull(json_contains_path(`opts`, 'one', '$.\"tags\"[-2]'), 0)"},
			lite: want{sql: `select * from "t" where json_type("opts", '$."tags"."2"') is not null and json_type("opts", '$."tags"[-2]') is not null`},
		},
		{
			name: "length",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonLength("opts->tags", ">", 2).OrWhereJsonLength("opts->tags", "3")
			}),
			pg:   want{sql: `select * from "t" where jsonb_array_length(("opts"->'tags')::jsonb) > $1 or jsonb_array_length(("opts"->'tags')::jsonb) = $2`, args: []any{int64(2), int64(3)}},
			my:   want{sql: "select * from `t` where json_length(`opts`, '$.\"tags\"') > ? or json_length(`opts`, '$.\"tags\"') = ?", args: []any{int64(2), int64(3)}},
			lite: want{sql: `select * from "t" where json_array_length("opts", '$."tags"') > ? or json_array_length("opts", '$."tags"') = ?`, args: []any{int64(2), int64(3)}},
		},
		{
			name: "overlaps mysql only",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).WhereJsonOverlaps("opts", []int{1, 2}).OrWhereJsonDoesntOverlap("opts->ids", []int{3})
			}),
			pg:   want{err: db.ErrUnsupported},
			my:   want{sql: "select * from `t` where json_overlaps(`opts`, ?) or not json_overlaps(json_extract(`opts`, '$.\"ids\"'), ?)", args: []any{"[1,2]", "[3]"}},
			lite: want{err: db.ErrUnsupported},
		},
		{
			name: "single quote in path cannot break out of the literal",
			build: selectOf(func(d db.Dialect) *db.Builder {
				return jt(d).Select(`opts->a\'b`)
			}),
			pg:   want{sql: `select "opts"->>'a\''b' from "t"`},
			my:   want{sql: "select json_unquote(json_extract(`opts`, '$.\"a''b\"')) from `t`"},
			lite: want{sql: `select json_extract("opts", '$."a''b"') from "t"`},
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
			pg:   want{sql: `update "t" set "name" = $1, "opts" = jsonb_set("opts"::jsonb, '{"theme"}', $2) where "id" = $3`, args: []any{"x", `"dark"`, 1}},
			my:   want{sql: "update `t` set `name` = ?, `opts` = json_set(`opts`, '$.\"theme\"', ?) where `id` = ?", args: []any{"x", "dark", 1}},
			lite: want{sql: `update "t" set "name" = ?, "opts" = json_patch(ifnull("opts", json('{}')), json(?)) where "id" = ?`, args: []any{"x", `{"theme":"dark"}`, 1}},
		},
		{
			name: "bool and container values",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("opts->on", true, "opts->tags", []string{"a"}))
			},
			pg:    want{sql: `update "t" set "opts" = jsonb_set(jsonb_set("opts"::jsonb, '{"on"}', $1), '{"tags"}', $2) where "id" = $3`, args: []any{"true", `["a"]`, 1}},
			my:    want{sql: "update `t` set `opts` = json_set(`opts`, '$.\"on\"', true, '$.\"tags\"', cast(? as json)) where `id` = ?", args: []any{`["a"]`, 1}},
			lite:  want{sql: `update "t" set "opts" = json_patch(ifnull("opts", json('{}')), json(?)) where "id" = ?`, args: []any{`{"on":true,"tags":["a"]}`, 1}},
			maria: want{sql: "update `t` set `opts` = json_set(`opts`, '$.\"on\"', true, '$.\"tags\"', json_query(?, '$')) where `id` = ?", args: []any{`["a"]`, 1}},
		},
		{
			name: "nested paths and array index",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("opts->a->b", 1, "opts->a->c", 2, "opts->d", 3))
			},
			pg:   want{sql: `update "t" set "opts" = jsonb_set(jsonb_set(jsonb_set("opts"::jsonb, '{"a","b"}', $1), '{"a","c"}', $2), '{"d"}', $3) where "id" = $4`, args: []any{"1", "2", "3", 1}},
			my:   want{sql: "update `t` set `opts` = json_set(`opts`, '$.\"a\".\"b\"', ?, '$.\"a\".\"c\"', ?, '$.\"d\"', ?) where `id` = ?", args: []any{1, 2, 3, 1}},
			lite: want{sql: `update "t" set "opts" = json_patch(ifnull("opts", json('{}')), json(?)) where "id" = ?`, args: []any{`{"a":{"b":1,"c":2},"d":3}`, 1}},
		},
		{
			name: "array index path and qualified column",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("t.opts->tags[0]", "x"))
			},
			pg:   want{sql: `update "t" set "opts" = jsonb_set("opts"::jsonb, '{"tags",0}', $1) where "id" = $2`, args: []any{`"x"`, 1}},
			my:   want{sql: "update `t` set `t`.`opts` = json_set(`t`.`opts`, '$.\"tags\"[0]', ?) where `id` = ?", args: []any{"x", 1}},
			lite: want{sql: `update "t" set "opts" = json_patch(ifnull("opts", json('{}')), json(?)) where "id" = ?`, args: []any{`{"tags[0]":"x"}`, 1}},
		},
		{
			name: "raw expression value",
			build: func(d db.Dialect) (string, []any, error) {
				return jt(d).Where("id", 1).UpdateSQL(m("opts->n", db.Raw("1 + 1")))
			},
			pg:   want{sql: `update "t" set "opts" = jsonb_set("opts"::jsonb, '{"n"}', 1 + 1) where "id" = $1`, args: []any{1}},
			my:   want{sql: "update `t` set `opts` = json_set(`opts`, '$.\"n\"', 1 + 1) where `id` = ?", args: []any{1}},
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
			assert.ErrorIs(t, err, tc.want)
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
	want := `select * from "t" where ("o"->'a')::jsonb @> $1 and not coalesce(("o")::jsonb ? 'b', false) or coalesce(("o")::jsonb ? 'c', false)`
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
	want := "select * from `t` where json_overlaps(`o`, ?) and not json_overlaps(json_extract(`o`, '$.\"x\"'), ?) or not json_overlaps(json_extract(`o`, '$.\"y\"'), ?)"
	if sql != want || !reflect.DeepEqual(args, []any{"[1]", "[2]", "[3]"}) {
		t.Errorf("got %s %v", sql, args)
	}
}
