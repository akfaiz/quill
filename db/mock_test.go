package db_test

import (
	"database/sql"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

// newMock opens a *sql.DB on go-sqlmock. Statements must match exactly (not as
// regular expressions) and every expectation must be met by the end of the test.
func newMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	conn, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = conn.Close()
	})
	return conn, mock
}

type person struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

// people returns n rows (1, "p1"), (2, "p2"), ...
func people(n int) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{"id", "name"})
	for i := 1; i <= n; i++ {
		rows.AddRow(int64(i), "p"+string(rune('0'+i)))
	}
	return rows
}
