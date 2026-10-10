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

func count(n int) *sqlmock.Rows { return sqlmock.NewRows([]string{"aggregate"}).AddRow(n) }

func TestPaginate(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	var out []person

	mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "people"`).WillReturnRows(count(5))
	mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 2 OFFSET 2`).WillReturnRows(people(2))
	p, err := conn.Table("people").OrderBy("id").Paginate(ctx, 2, 2, &out)
	require.NoError(t, err)
	assert.Equal(t, db.Page{Total: 5, PerPage: 2, CurrentPage: 2, LastPage: 3, From: 3, To: 4}, p)
	assert.Len(t, out, 2)

	t.Run("no rows", func(t *testing.T) {
		mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "people"`).WillReturnRows(count(0))
		p, err := conn.Table("people").Paginate(ctx, 1, 10, &out)
		require.NoError(t, err)
		assert.Equal(t, db.Page{PerPage: 10, CurrentPage: 1, LastPage: 1}, p)
		assert.NotNil(t, out)
		assert.Empty(t, out)
	})
	t.Run("page past the end", func(t *testing.T) {
		mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "people"`).WillReturnRows(count(3))
		mock.ExpectQuery(`SELECT * FROM "people" LIMIT 10 OFFSET 90`).WillReturnRows(people(0))
		p, err := conn.Table("people").Paginate(ctx, 10, 10, &out)
		require.NoError(t, err)
		assert.Equal(t, 0, p.From)
		assert.Equal(t, 0, p.To)
	})
	t.Run("page below one is clamped", func(t *testing.T) {
		mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "people"`).WillReturnRows(count(1))
		mock.ExpectQuery(`SELECT * FROM "people" LIMIT 10 OFFSET 0`).WillReturnRows(people(1))
		p, err := conn.Table("people").Paginate(ctx, -4, 10, &out)
		require.NoError(t, err)
		assert.Equal(t, 1, p.CurrentPage)
	})
	t.Run("errors", func(t *testing.T) {
		_, err := conn.Table("people").Paginate(ctx, 1, 0, &out)
		require.ErrorIs(t, err, db.ErrInvalidArgs)

		mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "people"`).WillReturnError(errors.New("boom"))
		_, err = conn.Table("people").Paginate(ctx, 1, 10, &out)
		require.Error(t, err)

		mock.ExpectQuery(`SELECT COUNT(*) AS "aggregate" FROM "people"`).WillReturnRows(count(2))
		mock.ExpectQuery(`SELECT * FROM "people" LIMIT 10 OFFSET 0`).WillReturnError(errors.New("boom"))
		_, err = conn.Table("people").Paginate(ctx, 1, 10, &out)
		require.Error(t, err)
	})
}

func TestSimplePaginate(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	var out []person

	mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 3 OFFSET 0`).WillReturnRows(people(3))
	p, err := conn.Table("people").OrderBy("id").SimplePaginate(ctx, 1, 2, &out)
	require.NoError(t, err)
	assert.Equal(t, db.SimplePage{PerPage: 2, CurrentPage: 1, HasMore: true}, p)
	assert.Len(t, out, 2, "the extra probe row is dropped")

	mock.ExpectQuery(`SELECT * FROM "people" LIMIT 3 OFFSET 2`).WillReturnRows(people(1))
	p, err = conn.Table("people").SimplePaginate(ctx, 2, 2, &out)
	require.NoError(t, err)
	assert.False(t, p.HasMore)

	_, err = conn.Table("people").SimplePaginate(ctx, 1, 0, &out)
	require.ErrorIs(t, err, db.ErrInvalidArgs)

	mock.ExpectQuery(`SELECT * FROM "people" LIMIT 3 OFFSET 0`).WillReturnError(errors.New("boom"))
	_, err = conn.Table("people").SimplePaginate(ctx, 1, 2, &out)
	require.Error(t, err)
}

func TestCursorPaginate(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	var out []person

	mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 3`).WillReturnRows(people(3))
	p, err := conn.Table("people").OrderBy("name").CursorPaginate(ctx, 2, "", &out)
	require.NoError(t, err)
	assert.True(t, p.HasMore)
	assert.NotEmpty(t, p.NextCursor)
	assert.Len(t, out, 2)
	afterTwo := p.NextCursor

	// the cursor round-trips into a keyset condition
	mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 3`).
		WithArgs(2).
		WillReturnRows(people(1))
	p, err = conn.Table("people").CursorPaginate(ctx, 2, p.NextCursor, &out)
	require.NoError(t, err)
	assert.False(t, p.HasMore)
	assert.Empty(t, p.NextCursor)

	t.Run("keyset condition applies to an existing OR as a whole", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" WHERE ("a" = $1 OR "b" = $2) AND "id" > $3 ORDER BY "id" ASC LIMIT 3`).
			WithArgs(1, 2, 2).
			WillReturnRows(people(1))
		_, err := conn.Table("people").Where("a", 1).OrWhere("b", 2).CursorPaginate(ctx, 2, afterTwo, &out)
		require.NoError(t, err)
	})

	t.Run("a union is rejected", func(t *testing.T) {
		q := conn.Table("people").Union(conn.Table("others"))
		_, err := q.CursorPaginate(ctx, 2, "", &out)
		require.ErrorIs(t, err, db.ErrInvalidArgs)
	})

	t.Run("descending on another column", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" DESC LIMIT 2`).WillReturnRows(people(1))
		_, err := conn.Table("people").CursorPaginate(ctx, 1, "", &out, db.CursorOptions{Desc: true})
		require.NoError(t, err)

		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "name" ASC LIMIT 2`).WillReturnRows(people(1))
		_, err = conn.Table("people").CursorPaginate(ctx, 1, "", &out, db.CursorOptions{Column: "name"})
		require.NoError(t, err)

		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" DESC LIMIT 3`).WillReturnRows(people(3))
		first, err := conn.Table("people").CursorPaginate(ctx, 2, "", &out, db.CursorOptions{Desc: true})
		require.NoError(t, err)
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" < $1 ORDER BY "id" DESC LIMIT 3`).
			WithArgs(2).
			WillReturnRows(people(1))
		_, err = conn.Table("people").CursorPaginate(ctx, 2, first.NextCursor, &out, db.CursorOptions{Desc: true})
		require.NoError(t, err)
	})
	t.Run("errors", func(t *testing.T) {
		_, err := conn.Table("people").CursorPaginate(ctx, 0, "", &out)
		require.ErrorIs(t, err, db.ErrInvalidArgs)

		_, err = conn.Table("people").CursorPaginate(ctx, 2, "not-a-cursor!!", &out)
		require.Error(t, err)

		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 3`).WillReturnError(errors.New("boom"))
		_, err = conn.Table("people").CursorPaginate(ctx, 2, "", &out)
		require.Error(t, err)

		// the keyset column must exist on the scanned rows
		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "nope" ASC LIMIT 3`).WillReturnRows(people(3))
		_, err = conn.Table("people").CursorPaginate(ctx, 2, "", &out, db.CursorOptions{Column: "nope"})
		require.Error(t, err)
	})
}
