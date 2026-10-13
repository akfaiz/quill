package db

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratedChainMethodsReturnReceiver calls every generated chain method with
// zero-value arguments: each must return its own receiver and never panic.
func TestGeneratedChainMethodsReturnReceiver(t *testing.T) {
	conn := Use(Postgres)
	receivers := map[string]func() any{
		"select": func() any { return conn.Select().From("users") },
		"insert": func() any { return conn.Insert("users") },
		"update": func() any { return conn.Update("users") },
		"delete": func() any { return conn.Delete("users") },
		"where":  func() any { return &WhereClause{q: &query{}} },
		"having": func() any { return &HavingClause{q: &query{}} },
		"typed":  func() any { return From[smokeRow](conn) },
	}
	for name, mk := range receivers {
		t.Run(name, func(t *testing.T) {
			typ := reflect.TypeOf(mk())
			calls := 0
			for i := range typ.NumMethod() {
				m := typ.Method(i)
				if m.Type.NumOut() != 1 || m.Type.Out(0) != typ || skipSmoke[m.Name] {
					continue
				}
				recv := reflect.ValueOf(mk())
				args := make([]reflect.Value, 0, m.Type.NumIn()-1)
				for j := 1; j < m.Type.NumIn(); j++ {
					args = append(args, reflect.Zero(m.Type.In(j)))
				}
				var out []reflect.Value
				require.NotPanics(t, func() {
					if m.Type.IsVariadic() {
						out = recv.Method(i).Call(args[:len(args)-1])
					} else {
						out = recv.Method(i).Call(args)
					}
				}, m.Name)
				assert.Equal(t, recv.Pointer(), out[0].Pointer(), m.Name)
				calls++
			}
			assert.Positive(t, calls)
		})
	}
}

func TestCloneNilAndIndependence(t *testing.T) {
	conn := Use(Postgres)
	assert.Nil(t, (*InsertBuilder)(nil).Clone())
	assert.Nil(t, (*UpdateBuilder)(nil).Clone())
	assert.Nil(t, (*DeleteBuilder)(nil).Clone())
	assert.Nil(t, (*SelectBuilder)(nil).Clone())

	u := conn.Update("users").Set("a", 1).Where("id", 1)
	u2 := u.Clone().Where("b", 2)
	sql1, _, err := u.ToSQL()
	require.NoError(t, err)
	sql2, _, err := u2.ToSQL()
	require.NoError(t, err)
	assert.NotEqual(t, sql1, sql2)

	d := conn.Delete("users").Where("id", 1)
	d2 := d.Clone().Where("b", 2)
	s1, _, err := d.ToSQL()
	require.NoError(t, err)
	s2, _, err := d2.ToSQL()
	require.NoError(t, err)
	assert.NotEqual(t, s1, s2)

	i := conn.Insert("users").Values(map[string]any{"a": 1})
	i2 := i.Clone().Returning("id")
	a, _, err := i.ToSQL()
	require.NoError(t, err)
	b, _, err := i2.ToSQL()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
}

func TestQueryHelpers(t *testing.T) {
	conn := Use(Postgres)
	q := From[smokeRow](conn).
		Apply(func(b *SelectBuilder) { b.Where("a", 1) }).
		When(false, func(q *Query[smokeRow]) { q.Where("skipped", 1) }).
		Unless(false, func(q *Query[smokeRow]) { q.Where("b", 2) })
	clone := q.Clone().Where("c", 3)
	got, args, err := q.Builder().ToSQL()
	require.NoError(t, err)
	assert.Equal(t, `SELECT * FROM "smoke_rows" WHERE "a" = $1 AND "b" = $2`, got)
	assert.Equal(t, []any{1, 2}, args)
	got2, _, err := clone.Builder().ToSQL()
	require.NoError(t, err)
	assert.Contains(t, got2, `"c" = $3`)
}

func TestToInt64(t *testing.T) {
	for _, v := range []any{int8(1), uint16(2), "3", 4} {
		_, err := toInt64(v)
		require.NoError(t, err)
	}
	_, err := toInt64(uint64(1) << 63)
	require.Error(t, err)
	_, err = toInt64(1.5)
	require.Error(t, err)
	_, err = toInt64("x")
	require.Error(t, err)
}

func TestClauseAccessorsNilExtra(t *testing.T) {
	c := &clause{}
	assert.Nil(t, c.cols())
	assert.Nil(t, c.kids())
	assert.Equal(t, Expression{}, c.rawExpr())
	assert.Empty(t, c.datePart())
}

func TestIsNilValue(t *testing.T) {
	var p *int
	assert.True(t, isNilValue(nil))
	assert.True(t, isNilValue(p))
	assert.False(t, isNilValue(1))
}

type smokeRow struct {
	ID int64 `db:"id"`
}

func (smokeRow) TableName() string { return "smoke_rows" }

func TestNilGroupCallbackRecordsError(t *testing.T) {
	_, _, err := Use(Postgres).Select().From("users").WhereGroup(nil).ToSQL()
	require.ErrorIs(t, err, ErrInvalidArgs)
}

// skipSmoke lists hand-written methods that run a caller-supplied callback, which a zero-value call cannot satisfy.
var skipSmoke = map[string]bool{"Clone": true, "Apply": true, "When": true, "Unless": true, "Tap": true}
