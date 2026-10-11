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

// member is the typed-API fixture: table "members", the id left out of writes when zero.
type member struct {
	ID   int64  `db:"id,omitempty"`
	Name string `db:"name"`
}

func (member) TableName() string { return "members" }

func members(n int) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "name"})
	for i := 1; i <= n; i++ {
		rows.AddRow(i, "m"+string(rune('0'+i)))
	}
	return rows
}

func TestTypedReads(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	q := func() *db.Query[member] { return db.From[member](conn) }

	mock.ExpectQuery(`SELECT * FROM "members"`).WillReturnRows(members(2))
	got, err := q().Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []member{{1, "m1"}, {2, "m2"}}, got)

	mock.ExpectQuery(`SELECT * FROM "members" LIMIT 1`).WillReturnRows(members(1))
	first, err := q().First(ctx)
	require.NoError(t, err)
	assert.Equal(t, member{1, "m1"}, first)

	mock.ExpectQuery(`SELECT * FROM "members" WHERE "id" = $1 LIMIT 1`).WithArgs(1).WillReturnRows(members(1))
	found, err := q().Find(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), found.ID)

	mock.ExpectQuery(`SELECT * FROM "members" LIMIT 2`).WillReturnRows(members(2))
	_, err = q().Sole(ctx)
	require.ErrorIs(t, err, db.ErrMultipleRecords)

	mock.ExpectQuery(`SELECT * FROM "members" LIMIT 2`).WillReturnRows(members(1))
	sole, err := q().Sole(ctx)
	require.NoError(t, err)
	assert.Equal(t, "m1", sole.Name)

	assert.NotNil(t, q().Builder())
}

func TestTypedPagination(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	q := func() *db.Query[member] { return db.From[member](conn).OrderBy("id") }

	mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "members"`).WillReturnRows(count(3))
	mock.ExpectQuery(`SELECT * FROM "members" ORDER BY "id" ASC LIMIT 2 OFFSET 2`).WillReturnRows(members(1))
	paged, err := q().Paginate(ctx, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), paged.Total)
	assert.Equal(t, 2, paged.LastPage)
	assert.Len(t, paged.Items, 1)

	mock.ExpectQuery(`SELECT * FROM "members" ORDER BY "id" ASC LIMIT 3 OFFSET 0`).WillReturnRows(members(3))
	simple, err := q().SimplePaginate(ctx, 1, 2)
	require.NoError(t, err)
	assert.True(t, simple.HasMore)
	assert.Len(t, simple.Items, 2)

	mock.ExpectQuery(`SELECT * FROM "members" ORDER BY "id" ASC LIMIT 3`).WillReturnRows(members(3))
	cursor, err := q().CursorPaginate(ctx, 2, "")
	require.NoError(t, err)
	assert.True(t, cursor.HasMore)
	assert.Len(t, cursor.Items, 2)
}

func TestTypedIteration(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	q := func() *db.Query[member] { return db.From[member](conn).OrderBy("id") }

	mock.ExpectQuery(`SELECT * FROM "members" ORDER BY "id" ASC LIMIT 2 OFFSET 0`).WillReturnRows(members(2))
	mock.ExpectQuery(`SELECT * FROM "members" ORDER BY "id" ASC LIMIT 2 OFFSET 2`).WillReturnRows(members(1))
	var sizes []int
	require.NoError(t, q().Chunk(ctx, 2, func(b []member) (bool, error) {
		sizes = append(sizes, len(b))
		return true, nil
	}))
	assert.Equal(t, []int{2, 1}, sizes)

	mock.ExpectQuery(`SELECT * FROM "members" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2`).
		WillReturnRows(members(2))
	mock.ExpectQuery(`SELECT * FROM "members" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 2`).
		WithArgs(2).
		WillReturnRows(members(1))
	sizes = nil
	require.NoError(t, q().ChunkByID(ctx, 2, func(b []member) (bool, error) {
		sizes = append(sizes, len(b))
		return true, nil
	}))
	assert.Equal(t, []int{2, 1}, sizes)

	mock.ExpectQuery(`SELECT * FROM "members" ORDER BY "id" ASC`).WillReturnRows(members(2))
	var names []string
	require.NoError(t, q().Each(ctx, func(m member) error { names = append(names, m.Name); return nil }))
	assert.Equal(t, []string{"m1", "m2"}, names)

	mock.ExpectQuery(`SELECT * FROM "members" ORDER BY "id" ASC`).WillReturnRows(members(2))
	names = nil
	for m, err := range q().Stream(ctx) {
		require.NoError(t, err)
		names = append(names, m.Name)
	}
	assert.Equal(t, []string{"m1", "m2"}, names)
}

func TestTypedRowsInsertReturning(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	// struct writes go through the write builders; Query[T] is select-only
	mock.ExpectQuery(`INSERT INTO "members" ("name") VALUES ($1) RETURNING *`).
		WithArgs("dee").
		WillReturnRows(members(1))
	var ins []member
	require.NoError(t, conn.Insert("members").Rows(member{Name: "dee"}).Returning("*").Get(ctx, &ins))
	assert.Len(t, ins, 1)

	mock.ExpectQuery(`UPDATE "members" SET "name" = $1 WHERE "id" = $2 RETURNING *`).
		WithArgs("ann", 1).
		WillReturnRows(members(1))
	var upd []member
	require.NoError(t, conn.Update("members").SetRow(member{Name: "ann"}).Where("id", 1).Returning("*").Get(ctx, &upd))
	assert.Len(t, upd, 1)

	mock.ExpectQuery(`DELETE FROM "members" WHERE "id" = $1 RETURNING "id"`).WithArgs(2).WillReturnRows(members(1))
	var del []member
	require.NoError(t, conn.Delete("members").Where("id", 2).Returning("id").Get(ctx, &del))
	assert.Len(t, del, 1)

	// a non-struct row cannot be converted to columns
	require.Error(t, conn.Insert("t").Rows(1).Returning("*").Get(ctx, &ins))
	require.Error(t, conn.Update("t").SetRow(1).Where("id", 1).Returning("*").Get(ctx, &upd))
}

func TestTypedScalarHelpers(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT "name" FROM "members" WHERE "id" = $1 LIMIT 1`).WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("ann"))
	name, err := db.ValueAs[string](ctx, conn.Select().From("members").Where("id", 1), "name")
	require.NoError(t, err)
	assert.Equal(t, "ann", name)

	mock.ExpectQuery(`SELECT "id" FROM "members"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))
	ids, err := db.PluckAs[int64](ctx, conn.Select().From("members"), "id")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2}, ids)

	mock.ExpectQuery(`SELECT "name" FROM "members" LIMIT 1`).WillReturnError(errors.New("boom"))
	_, err = db.ValueAs[string](ctx, conn.Select().From("members"), "name")
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
