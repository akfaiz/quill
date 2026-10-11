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

func TestChunk(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 2 OFFSET 0`).WillReturnRows(people(2))
	mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 2 OFFSET 2`).WillReturnRows(people(1))
	var batch []person
	var sizes []int
	err := conn.Select().From("people").OrderBy("id").Chunk(ctx, 2, &batch, func() (bool, error) {
		sizes = append(sizes, len(batch))
		return true, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, sizes)

	t.Run("an empty page ends it", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 2 OFFSET 0`).WillReturnRows(people(0))
		called := false
		require.NoError(t, conn.Select().From("people").OrderBy("id").Chunk(ctx, 2, &batch, func() (bool, error) {
			called = true
			return true, nil
		}))
		assert.False(t, called)
	})
	t.Run("false stops early", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 2 OFFSET 0`).WillReturnRows(people(2))
		require.NoError(t, conn.Select().From("people").OrderBy("id").Chunk(ctx, 2, &batch,
			func() (bool, error) { return false, nil }))
	})
	t.Run("callback error is returned", func(t *testing.T) {
		boom := errors.New("boom")
		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 2 OFFSET 0`).WillReturnRows(people(2))
		err := conn.Select().
			From("people").
			OrderBy("id").
			Chunk(ctx, 2, &batch, func() (bool, error) { return true, boom })
		require.ErrorIs(t, err, boom)
	})
	t.Run("query error is returned", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC LIMIT 2 OFFSET 0`).
			WillReturnError(errors.New("boom"))
		err := conn.Select().
			From("people").
			OrderBy("id").
			Chunk(ctx, 2, &batch, func() (bool, error) { return true, nil })
		require.Error(t, err)
	})
	t.Run("guards", func(t *testing.T) {
		fn := func() (bool, error) { return true, nil }
		require.ErrorIs(t, conn.Select().From("people").Chunk(ctx, 2, &batch, fn), db.ErrNoOrder)
		require.ErrorIs(t, conn.Select().From("people").OrderBy("id").Chunk(ctx, 0, &batch, fn), db.ErrInvalidArgs)
	})
}

func TestChunkByID(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)
	var batch []person

	mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2`).
		WillReturnRows(people(2))
	mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 2`).
		WithArgs(2).
		WillReturnRows(people(1))
	var sizes []int
	require.NoError(t, conn.Select().From("people").ChunkByID(ctx, 2, &batch, func() (bool, error) {
		sizes = append(sizes, len(batch))
		return true, nil
	}))
	assert.Equal(t, []int{2, 1}, sizes)

	t.Run("callback may truncate or reorder the batch", func(t *testing.T) {
		for name, edit := range map[string]func(){
			"truncate": func() { batch = batch[:0] },
			"reverse":  func() { batch[0], batch[1] = batch[1], batch[0] },
		} {
			t.Run(name, func(t *testing.T) {
				mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2`).
					WillReturnRows(people(2))
				// the next page still starts after id 2, the last row as fetched
				mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 2`).
					WithArgs(2).
					WillReturnRows(people(0))
				require.NotPanics(t, func() {
					require.NoError(t, conn.Select().From("people").ChunkByID(ctx, 2, &batch, func() (bool, error) {
						if len(batch) == 2 {
							edit()
						}
						return true, nil
					}))
				})
			})
		}
	})

	t.Run("user order by is replaced by the key order", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2`).
			WillReturnRows(people(2))
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 2`).
			WithArgs(2).
			WillReturnRows(people(0))
		require.NoError(t, conn.Select().From("people").OrderBy("name").OrderByDesc("id").
			ChunkByID(ctx, 2, &batch, func() (bool, error) { return true, nil }))
	})
	t.Run("a union is rejected", func(t *testing.T) {
		q := conn.Select().From("people").Union(conn.Select().From("others"))
		err := q.ChunkByID(ctx, 2, &batch, func() (bool, error) { return true, nil })
		require.ErrorIs(t, err, db.ErrInvalidArgs)
	})

	t.Run("custom column", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "name" IS NOT NULL ORDER BY "name" ASC LIMIT 5`).
			WillReturnRows(people(0))
		require.NoError(t, conn.Select().From("people").ChunkByID(ctx, 5, &batch,
			func() (bool, error) { return true, nil }, "name"))
	})
	t.Run("stop and callback error", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2`).
			WillReturnRows(people(2))
		require.NoError(
			t,
			conn.Select().From("people").ChunkByID(ctx, 2, &batch, func() (bool, error) { return false, nil }),
		)

		boom := errors.New("boom")
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2`).
			WillReturnRows(people(2))
		err := conn.Select().From("people").ChunkByID(ctx, 2, &batch, func() (bool, error) { return true, boom })
		require.ErrorIs(t, err, boom)
	})
	t.Run("query error and bad size", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 2`).
			WillReturnError(errors.New("boom"))
		require.Error(
			t,
			conn.Select().From("people").ChunkByID(ctx, 2, &batch, func() (bool, error) { return true, nil }),
		)
		require.ErrorIs(t, conn.Select().From("people").ChunkByID(ctx, 0, &batch, nil), db.ErrInvalidArgs)
	})
	t.Run("NULL key", func(t *testing.T) {
		var nullable []struct {
			ID *int64 `db:"id"`
		}
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "id" IS NOT NULL ORDER BY "id" ASC LIMIT 1`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(nil))
		err := conn.Select().From("people").ChunkByID(ctx, 1, &nullable, func() (bool, error) { return true, nil })
		require.ErrorIs(t, err, db.ErrInvalidArgs)
	})
	t.Run("missing key column", func(t *testing.T) {
		mock.ExpectQuery(`SELECT * FROM "people" WHERE "nope" IS NOT NULL ORDER BY "nope" ASC LIMIT 1`).
			WillReturnRows(people(1))
		err := conn.Select().From("people").ChunkByID(ctx, 1, &batch, func() (bool, error) { return true, nil }, "nope")
		require.Error(t, err)
	})
}

func TestEach(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT * FROM "people" ORDER BY "id" ASC`).WillReturnRows(people(2))
	var cur person
	var seen []person
	require.NoError(t, conn.Select().From("people").OrderBy("id").Each(ctx, &cur, func() error {
		seen = append(seen, cur)
		return nil
	}))
	assert.Equal(t, []person{{1, "p1"}, {2, "p2"}}, seen)

	require.ErrorIs(t, conn.Select().From("people").Each(ctx, person{}, nil), db.ErrInvalidArgs)
	require.ErrorIs(t, conn.Select().From("people").Each(ctx, (*person)(nil), nil), db.ErrInvalidArgs)

	boom := errors.New("boom")
	mock.ExpectQuery(`SELECT * FROM "people"`).WillReturnRows(people(2))
	require.ErrorIs(t, conn.Select().From("people").Each(ctx, &cur, func() error { return boom }), boom)

	mock.ExpectQuery(`SELECT * FROM "people"`).WillReturnRows(people(1).RowError(0, errors.New("row boom")))
	require.Error(t, conn.Select().From("people").Each(ctx, &cur, func() error { return nil }))

	// an unmappable destination fails when the mapper is built
	var bad int
	mock.ExpectQuery(`SELECT * FROM "people"`).WillReturnRows(people(1))
	require.Error(t, conn.Select().From("people").Each(ctx, &bad, func() error { return nil }))
}

func TestStream(t *testing.T) {
	ctx := context.Background()
	sqlDB, mock := newMock(t)
	conn := db.New(sqlDB, db.Postgres)

	mock.ExpectQuery(`SELECT * FROM "people"`).WillReturnRows(people(3))
	var got []person
	for p, err := range db.Stream[person](ctx, conn.Select().From("people")) {
		require.NoError(t, err)
		got = append(got, p)
	}
	assert.Len(t, got, 3)

	mock.ExpectQuery(`SELECT * FROM "people"`).WillReturnRows(people(3))
	got = nil
	for p, err := range db.Stream[person](ctx, conn.Select().From("people")) {
		require.NoError(t, err)
		got = append(got, p)
		break
	}
	assert.Len(t, got, 1)

	mock.ExpectQuery(`SELECT * FROM "people"`).WillReturnError(errors.New("boom"))
	var errs []error
	for _, err := range db.Stream[person](ctx, conn.Select().From("people")) {
		errs = append(errs, err)
	}
	require.Len(t, errs, 1)
	require.Error(t, errs[0])
}
