package db_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/db"
)

func TestInsertUsingIgnoreAndErrors(t *testing.T) {
	conn := db.Use(db.Postgres)
	sub := conn.Select("id", "name").From("staging")

	got, _, err := conn.Insert("users").Using([]string{"id", "name"}, sub).Ignore().ToSQL()
	require.NoError(t, err)
	assert.Equal(t, `INSERT INTO "users" ("id", "name") SELECT "id", "name" FROM "staging" ON CONFLICT DO NOTHING`, got)

	_, _, err = conn.Insert("users").Using(nil, nil).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)

	_, _, err = conn.Insert("users").Using(nil, sub).Ignore().Upsert([]string{"id"}, []string{"name"}).ToSQL()
	require.ErrorIs(t, err, db.ErrInvalidArgs)
}
