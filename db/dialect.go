package db

import "github.com/akfaiz/quill/internal/dialect"

// Dialect selects the SQL grammar used to compile queries.
type Dialect int

// The supported dialects. The zero Dialect is invalid and returns ErrUnknownDialect.
const (
	Postgres Dialect = iota + 1
	MySQL
	SQLite
	// MariaDB shares MySQL's grammar and differs in two places: it has no lateral
	// joins, and it has no CAST AS JSON (json_query is used for JSON container values).
	MariaDB
)

// mysqlFamily reports the dialects that share MySQL's SQL syntax.
func (d Dialect) mysqlFamily() bool { return d == MySQL || d == MariaDB }

// String returns the lower-case dialect name ("postgres", "mysql", "sqlite", "mariadb"),
// or "unknown" for any other value.
//
// Example:
//
//	db.MariaDB.String() // "mariadb"; db.Dialect(0).String() is "unknown"
func (d Dialect) String() string {
	switch d {
	case Postgres:
		return "postgres"
	case MySQL:
		return "mysql"
	case SQLite:
		return "sqlite"
	case MariaDB:
		return "mariadb"
	}
	return "unknown"
}

func (d Dialect) grammar() (dialect.Grammar, error) {
	switch d {
	case Postgres:
		return dialect.Postgres(), nil
	case MySQL:
		return dialect.MySQL(), nil
	case SQLite:
		return dialect.SQLite(), nil
	case MariaDB:
		return dialect.MariaDB(), nil
	}
	return nil, ErrUnknownDialect
}
