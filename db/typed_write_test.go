package db_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func TestTypedWrites(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	q := func() *db.Query[member] { return db.From[member](conn) }

	mock.ExpectExec(`INSERT INTO "members" ("name") VALUES ($1), ($2)`).WithArgs("ann", "bob").
		WillReturnResult(sqlmock.NewResult(0, 2))
	n, err := q().Insert(ctx, member{Name: "ann"}, member{Name: "bob"})
	require.NoError(t, err)
	assert.EqualValues(t, 2, n)

	mock.ExpectExec(`INSERT INTO "members" ("id", "name") VALUES ($1, $2) ON CONFLICT DO NOTHING`).WithArgs(1, "ann").
		WillReturnResult(sqlmock.NewResult(0, 0))
	n, err = q().InsertOrIgnore(ctx, member{ID: 1, Name: "ann"})
	require.NoError(t, err)
	assert.Zero(t, n)

	mock.ExpectQuery(`INSERT INTO "members" ("name") VALUES ($1) RETURNING "id"`).WithArgs("cy").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	id, err := q().InsertGetID(ctx, member{Name: "cy"})
	require.NoError(t, err)
	assert.EqualValues(t, 3, id)

	mock.ExpectExec(`INSERT INTO "members" ("id", "name") VALUES ($1, $2) ON CONFLICT ("id") DO UPDATE SET "name" = "excluded"."name"`).
		WithArgs(1, "ann").
		WillReturnResult(sqlmock.NewResult(0, 1))
	n, err = q().Upsert(ctx, []member{{ID: 1, Name: "ann"}}, []string{"id"}, []string{"name"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	mock.ExpectExec(`UPDATE "members" SET "name" = $1 WHERE "id" = $2`).WithArgs("ann", 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	n, err = q().Where("id", 1).Update(ctx, member{Name: "ann"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	mock.ExpectExec(`UPDATE "members" SET "name" = upper(name) WHERE "id" = $1`).WithArgs(1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	_, err = q().Where("id", 1).UpdateMap(ctx, map[string]any{"name": db.Raw("upper(name)")})
	require.NoError(t, err)

	mock.ExpectExec(`DELETE FROM "members" WHERE "id" = $1`).WithArgs(2).WillReturnResult(sqlmock.NewResult(0, 1))
	_, err = q().Where("id", 2).Delete(ctx)
	require.NoError(t, err)

	mock.ExpectExec(`TRUNCATE "members" RESTART IDENTITY`).WillReturnResult(sqlmock.NewResult(0, 0))
	require.NoError(t, q().Truncate(ctx))
}

func TestTypedWritesRejectNonStructs(t *testing.T) {
	ctx := context.Background()
	sqlDB, _ := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	q := db.As[int](conn.Table("t"))

	_, err := q.Insert(ctx, 1)
	require.Error(t, err)
	_, err = q.InsertOrIgnore(ctx, 1)
	require.Error(t, err)
	_, err = q.InsertGetID(ctx, 1)
	require.Error(t, err)
	_, err = q.Upsert(ctx, []int{1}, []string{"id"}, nil)
	require.Error(t, err)
	_, err = q.Update(ctx, 1)
	require.Error(t, err)
}

func TestTypedAggregates(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	q := func() *db.Query[member] { return db.From[member](conn) }
	agg := func(v any) *sqlmock.Rows { return sqlmock.NewRows([]string{"aggregate"}).AddRow(v) }
	exists := func(v bool) *sqlmock.Rows { return sqlmock.NewRows([]string{"exists"}).AddRow(v) }

	mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "members" WHERE "age" > $1`).
		WithArgs(18).
		WillReturnRows(agg(3))
	n, err := q().Where("age", ">", 18).Count(ctx)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n)

	mock.ExpectQuery(`SELECT EXISTS(SELECT * FROM "members") AS "exists"`).WillReturnRows(exists(true))
	ok, err := q().Exists(ctx)
	require.NoError(t, err)
	assert.True(t, ok)

	mock.ExpectQuery(`SELECT EXISTS(SELECT * FROM "members") AS "exists"`).WillReturnRows(exists(true))
	none, err := q().DoesntExist(ctx)
	require.NoError(t, err)
	assert.False(t, none)

	mock.ExpectQuery(`SELECT SUM("age") AS "aggregate" FROM "members"`).WillReturnRows(agg(86.0))
	sum, err := q().Sum(ctx, "age")
	require.NoError(t, err)
	assert.InDelta(t, 86, sum, 0)

	mock.ExpectQuery(`SELECT AVG("age") AS "aggregate" FROM "members"`).WillReturnRows(agg(28.5))
	avg, err := q().Avg(ctx, "age")
	require.NoError(t, err)
	assert.InDelta(t, 28.5, avg, 0)
}
