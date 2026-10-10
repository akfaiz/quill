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

func TestTypedReturning(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`INSERT INTO "members" ("name") VALUES ($1) RETURNING *`).
		WithArgs("dee").
		WillReturnRows(members(1))
	ins, err := db.From[member](conn).InsertReturning(ctx, member{Name: "dee"})
	require.NoError(t, err)
	assert.Len(t, ins, 1)

	mock.ExpectQuery(`UPDATE "members" SET "name" = $1 WHERE "id" = $2 RETURNING *`).
		WithArgs("ann", 1).
		WillReturnRows(members(1))
	upd, err := db.From[member](conn).Where("id", 1).UpdateReturning(ctx, member{Name: "ann"})
	require.NoError(t, err)
	assert.Len(t, upd, 1)

	mock.ExpectQuery(`DELETE FROM "members" WHERE "id" = $1 RETURNING "id"`).WithArgs(2).WillReturnRows(members(1))
	del, err := db.From[member](conn).Where("id", 2).Returning("id").DeleteReturning(ctx)
	require.NoError(t, err)
	assert.Len(t, del, 1)

	mock.ExpectQuery(`DELETE FROM "members" WHERE "id" = $1 RETURNING *`).WithArgs(2).WillReturnRows(members(1))
	_, err = db.From[member](conn).Where("id", 2).DeleteReturning(ctx)
	require.NoError(t, err)

	// a non-struct row cannot be converted to columns
	_, err = db.As[int](conn.Table("t")).InsertReturning(ctx, 1)
	require.Error(t, err)
	_, err = db.As[int](conn.Table("t")).UpdateReturning(ctx, 1)
	require.Error(t, err)
}

func TestTypedScalarHelpers(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT "name" FROM "members" WHERE "id" = $1 LIMIT 1`).WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("ann"))
	name, err := db.ValueAs[string](ctx, conn.Table("members").Where("id", 1), "name")
	require.NoError(t, err)
	assert.Equal(t, "ann", name)

	mock.ExpectQuery(`SELECT "id" FROM "members"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))
	ids, err := db.PluckAs[int64](ctx, conn.Table("members"), "id")
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2}, ids)

	agg := func(v any) *sqlmock.Rows { return sqlmock.NewRows([]string{"aggregate"}).AddRow(v) }
	mock.ExpectQuery(`SELECT AVG("age") AS "aggregate" FROM "members"`).WillReturnRows(agg(3.5))
	avg, err := db.AggregateAs[float64](ctx, conn.Table("members"), "avg", "age")
	require.NoError(t, err)
	assert.InDelta(t, 3.5, avg, 0)

	mock.ExpectQuery(`SELECT MIN("age") AS "aggregate" FROM "members"`).WillReturnRows(agg(18))
	lo, err := db.MinAs[int](ctx, conn.Table("members"), "age")
	require.NoError(t, err)
	assert.Equal(t, 18, lo)

	mock.ExpectQuery(`SELECT MAX("age") AS "aggregate" FROM "members"`).WillReturnRows(agg(60))
	hi, err := db.MaxAs[int](ctx, conn.Table("members"), "age")
	require.NoError(t, err)
	assert.Equal(t, 60, hi)

	mock.ExpectQuery(`SELECT "name" FROM "members" LIMIT 1`).WillReturnError(errors.New("boom"))
	_, err = db.ValueAs[string](ctx, conn.Table("members"), "name")
	require.Error(t, err)
}
