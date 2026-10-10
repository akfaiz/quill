package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func TestGetFirstFindSole(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT * FROM "people" WHERE "age" > $1`).WithArgs(3).WillReturnRows(people(2))
	var all []person
	require.NoError(t, conn.Table("people").Where("age", ">", 3).Get(ctx, &all))
	assert.Equal(t, []person{{1, "p1"}, {2, "p2"}}, all)

	mock.ExpectQuery(`SELECT * FROM "people" LIMIT 1`).WillReturnRows(people(1))
	var one person
	require.NoError(t, conn.Table("people").First(ctx, &one))
	assert.Equal(t, person{1, "p1"}, one)

	mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" = $1 LIMIT 1`).WithArgs(9).WillReturnRows(people(1))
	require.NoError(t, conn.Table("people").Find(ctx, 9, &one))

	mock.ExpectQuery(`SELECT * FROM "people" LIMIT 1`).WillReturnRows(people(0))
	require.ErrorIs(t, conn.Table("people").First(ctx, &one), db.ErrNotFound)

	mock.ExpectQuery(`SELECT * FROM "people" LIMIT 2`).WillReturnRows(people(1))
	require.NoError(t, conn.Table("people").Sole(ctx, &one))

	mock.ExpectQuery(`SELECT * FROM "people" LIMIT 2`).WillReturnRows(people(2))
	require.ErrorIs(t, conn.Table("people").Sole(ctx, &one), db.ErrMultipleRecords)

	mock.ExpectQuery(`SELECT * FROM "people" LIMIT 2`).WillReturnRows(people(0))
	require.ErrorIs(t, conn.Table("people").Sole(ctx, &one), db.ErrNotFound)

	// the id applies to the whole existing condition, not only its last OR branch
	mock.ExpectQuery(`SELECT * FROM "people" WHERE ("age" > $1 OR "vip" = $2) AND "id" = $3 LIMIT 1`).
		WithArgs(3, true, 9).
		WillReturnRows(people(1))
	require.NoError(t, conn.Table("people").Where("age", ">", 3).OrWhere("vip", true).Find(ctx, 9, &one))
}

func TestGetPropagatesBuildAndDriverErrors(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	boom := errors.New("boom")

	mock.ExpectQuery(`SELECT * FROM "people"`).WillReturnError(boom)
	var all []person
	require.ErrorIs(t, conn.Table("people").Get(ctx, &all), boom)

	// a bad builder argument surfaces before anything is sent (no expectation set)
	require.Error(t, conn.Table("people").Where("a", "bogus-op", 1).Get(ctx, &all))
}

func TestValueAndPluck(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT "name" FROM "people" WHERE "id" = $1 LIMIT 1`).WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("ann"))
	var name string
	require.NoError(t, conn.Table("people").Where("id", 1).Value(ctx, "name", &name))
	assert.Equal(t, "ann", name)

	mock.ExpectQuery(`SELECT "name" FROM "people"`).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("ann").AddRow("bob"))
	var names []string
	require.NoError(t, conn.Table("people").Pluck(ctx, "name", &names))
	assert.Equal(t, []string{"ann", "bob"}, names)
}

func TestPluckMap(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT "name", "id" FROM "people"`).
		WillReturnRows(sqlmock.NewRows([]string{"name", "id"}).AddRow("ann", 1).AddRow("bob", 2))
	var byID map[int64]string
	require.NoError(t, conn.Table("people").PluckMap(ctx, "name", "id", &byID))
	assert.Equal(t, map[int64]string{1: "ann", 2: "bob"}, byID)

	var notMap []string
	require.ErrorIs(t, conn.Table("people").PluckMap(ctx, "name", "id", &notMap), db.ErrInvalidArgs)
	require.ErrorIs(t, conn.Table("people").PluckMap(ctx, "name", "id", nil), db.ErrInvalidArgs)

	mock.ExpectQuery(`SELECT "name", "id" FROM "people"`).WillReturnError(errors.New("boom"))
	require.Error(t, conn.Table("people").PluckMap(ctx, "name", "id", &byID))
}

func TestExists(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	exists := func(v bool) *sqlmock.Rows { return sqlmock.NewRows([]string{"exists"}).AddRow(v) }

	mock.ExpectQuery(`SELECT EXISTS(SELECT * FROM "people" WHERE "id" = $1) AS "exists"`).WithArgs(1).
		WillReturnRows(exists(true))
	ok, err := conn.Table("people").Where("id", 1).Exists(ctx)
	require.NoError(t, err)
	assert.True(t, ok)

	mock.ExpectQuery(`SELECT EXISTS(SELECT * FROM "people") AS "exists"`).WillReturnRows(exists(false))
	none, err := conn.Table("people").DoesntExist(ctx)
	require.NoError(t, err)
	assert.True(t, none)

	mock.ExpectQuery(`SELECT EXISTS(SELECT * FROM "people") AS "exists"`).WillReturnError(errors.New("boom"))
	_, err = conn.Table("people").Exists(ctx)
	require.Error(t, err)

	_, err = db.Use(db.Postgres).Table("people").Exists(ctx)
	require.ErrorIs(t, err, db.ErrNotBound)
	_, err = conn.Table("people").Where("a", "bogus-op", 1).Exists(ctx)
	require.Error(t, err)
}

func TestExistsSQLFailsOnBadBuilder(t *testing.T) {
	_, _, err := db.Use(db.Postgres).Table("t").Where("a", "bogus-op", 1).ExistsSQL()
	require.Error(t, err)
	_, _, err = db.Use(db.Dialect(99)).Table("t").ExistsSQL()
	require.ErrorIs(t, err, db.ErrUnknownDialect)
}

func TestAggregates(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	agg := func(v any) *sqlmock.Rows { return sqlmock.NewRows([]string{"aggregate"}).AddRow(v) }

	mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "orders"`).WillReturnRows(agg(7))
	n, err := conn.Table("orders").Count(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 7, n)

	mock.ExpectQuery(`SELECT COUNT("user_id") AS "aggregate" FROM "orders"`).WillReturnRows(agg(3))
	n, err = conn.Table("orders").Count(ctx, "user_id")
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)

	mock.ExpectQuery(`SELECT SUM("total") AS "aggregate" FROM "orders"`).WillReturnRows(agg(12.5))
	sum, err := conn.Table("orders").Sum(ctx, "total")
	require.NoError(t, err)
	assert.InDelta(t, 12.5, sum, 0)

	mock.ExpectQuery(`SELECT AVG("total") AS "aggregate" FROM "orders"`).WillReturnRows(agg(2.5))
	avg, err := conn.Table("orders").Avg(ctx, "total")
	require.NoError(t, err)
	assert.InDelta(t, 2.5, avg, 0)

	var lo, hi int64
	mock.ExpectQuery(`SELECT MIN("total") AS "aggregate" FROM "orders"`).WillReturnRows(agg(1))
	require.NoError(t, conn.Table("orders").Min(ctx, "total", &lo))
	mock.ExpectQuery(`SELECT MAX("total") AS "aggregate" FROM "orders"`).WillReturnRows(agg(9))
	require.NoError(t, conn.Table("orders").Max(ctx, "total", &hi))
	assert.EqualValues(t, 1, lo)
	assert.EqualValues(t, 9, hi)

	_, err = db.Use(db.Postgres).Table("orders").Count(ctx)
	require.ErrorIs(t, err, db.ErrNotBound)
	_, err = conn.Table("orders").Where("a", "bogus-op", 1).Count(ctx)
	require.Error(t, err)
}

func TestAggregateSQLWrapsGroupedQueriesAndStripsQualifiers(t *testing.T) {
	q := db.Use(db.Postgres).Table("orders").GroupBy("user_id")
	sqlText, _, err := q.AggregateSQL("max", "o.total")
	require.NoError(t, err)
	assert.Equal(
		t,
		`SELECT MAX("total") AS "aggregate" FROM (SELECT * FROM "orders" GROUP BY "user_id") AS "aggregate_table"`,
		sqlText,
	)

	sqlText, _, err = q.AggregateSQL("count", "")
	require.NoError(t, err)
	assert.Contains(t, sqlText, `COUNT(*) AS "aggregate"`)

	sqlText, _, err = db.Use(db.Postgres).Table("orders").Distinct().AggregateSQL("count", "t.total as x")
	require.NoError(t, err)
	assert.Contains(t, sqlText, `COUNT("x")`)

	_, _, err = db.Use(db.Dialect(99)).Table("orders").AggregateSQL("sum", "x")
	require.ErrorIs(t, err, db.ErrUnknownDialect)
}
