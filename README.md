# quill

A fluent SQL query builder for Go, modelled on Laravel's query builder. It builds SQL with bind
arguments and can optionally run it through `database/sql`. **It is not an ORM**: there are no
models, relations, hooks or migrations.

- Dialects: PostgreSQL, MySQL, MariaDB, SQLite
- Standard library only at runtime (no third-party dependencies; testify is used by tests only)
- Go 1.25 or newer

```go
import "github.com/akfaiz/quill/db"
```

## Features

**Building queries**

- `SELECT` with aliases, `DISTINCT`, aggregates, `GROUP BY` / `HAVING`, ordering, limit and offset
- Rich `WHERE`: comparisons, `IN`, `BETWEEN`, `LIKE`, `NULL`, column and date-part comparisons,
  nested groups, `EXISTS`, row values, and `WhereAll` / `WhereAny` / `WhereNone`
- Joins (including subquery and lateral joins), unions, subqueries and common table expressions
  (`WITH`, `WITH RECURSIVE`)
- JSON predicates and JSON path updates, locks and index hints, raw SQL escape hatches
- Conditional building with `When` and `Tap`; `Clone` and snapshotting keep reused builders safe
- Inspect SQL with `ToSQL`, or with arguments interpolated through `ToRawSQL` (debugging only)

**Writing data**

- Insert, insert-or-ignore, insert from a subquery, upsert, update, increment / decrement, delete,
  truncate and `RETURNING`
- Struct writes with `Row` / `Rows` and `db:"name,omitempty"` tags

**Running queries** (optional: build-only use needs no connection)

- Scan into structs, maps and scalars; `First`, `Find`, `Sole`, `Value`, `Pluck`, `Exists`
- Offset, simple and cursor (keyset) pagination; `Chunk` and `Each` for large result sets
- Transactions, a query hook for logging, metrics and tracing, and a table prefix option
- Typed API with `From[T]`, so you get `[]T` back without a destination variable

## Why quill

- **Not an ORM.** No models, relations, hooks or migrations; you keep control of the SQL.
- **One API, four dialects.** Quoting, placeholders and syntax differences are handled for you, and
  a feature a dialect cannot express returns `ErrUnsupported` instead of emitting SQL that
  behaves differently.
- **Safe by default.** Values are always bound, identifiers are always quoted, and an
  `UPDATE` or `DELETE` without a `WHERE` is refused unless you call `AllRows()`.
- **Errors, not panics.** A bad argument is recorded and returned from the terminal call. Only the
  `Must*` variants panic.
- **Fast to build.** Query construction is several times faster than squirrel and goqu, with far
  fewer allocations; see the [benchmarks](test/benchmark/README.md).

## Install

```sh
go get github.com/akfaiz/quill
```

## Quick start

Build SQL without a database (set the dialect first, then chain from `Table`):

```go
sql, args, err := db.Use(db.Postgres).Table("users").
    Select("id", "name").
    Where("votes", ">", 100).
    OrderByDesc("created_at").
    Limit(10).
    ToSQL()
// select "id", "name" from "users" where "votes" > $1 order by "created_at" desc limit 10
// args: [100]
```

Run queries (wrap any `*sql.DB`, `*sql.Tx` or `*sql.Conn`):

```go
conn := db.New(sqlDB, db.Postgres)

var users []User
err := conn.Table("users").Where("active", true).OrderBy("id").Get(ctx, &users)
```

Let a struct name its own table, and skip the destination variable:

```go
type User struct {
    ID    int64   `db:"id,omitempty"`
    Name  string  `db:"name"`
    Email *string `db:"email"`
}

func (User) TableName() string { return "users" }

users, err := db.From[User](conn).Where("active", true).OrderBy("id").Get(ctx) // []User
id, err    := db.From[User](conn).InsertGetID(ctx, User{Name: "Ann"})
_, err      = db.From[User](conn).Where("id", id).Update(ctx, User{Name: "Anna"})
```

Run several statements atomically:

```go
err := conn.Transaction(ctx, func(tx *db.DB) error {
    if _, err := tx.Table("accounts").Where("id", 1).Decrement(ctx, "balance", 50, nil); err != nil {
        return err
    }
    _, err := tx.Table("accounts").Where("id", 2).Increment(ctx, "balance", 50, nil)
    return err
})
```

> Name the variable `conn` (or anything but `db`): a local variable called `db` hides the
> package inside its scope.

## Documentation

| Guide | What it covers |
| --- | --- |
| [Getting started](docs/getting-started.md) | Install, connecting, options, building versus running, errors |
| [Building queries](docs/building-queries.md) | Select, where, joins, grouping, ordering, unions, subqueries, JSON, raw SQL |
| [Writing data](docs/writing-data.md) | Insert, upsert, update, increment, delete, truncate, `RETURNING`, the safety guard |
| [Running queries](docs/running-queries.md) | Reading and scanning, aggregates, pagination, chunking, streaming, transactions, hooks |
| [Typed API](docs/typed-api.md) | `TableName()`, `From[T]`, `As[T]`, struct writes, scalar helpers |
| [Dialects](docs/dialects.md) | What differs between PostgreSQL, MySQL, MariaDB and SQLite, with versions |
| [Security](docs/security.md) | Injection, raw SQL, logging and personal data, untrusted cursors |
| [Reference](docs/reference.md) | Errors, types, options and an index of every method |
| [Benchmarks](test/benchmark/README.md) | Build-time comparison with squirrel, goqu and go-sqlbuilder |
| [Contributing](CONTRIBUTING.md) | Layout, tests, fuzzing, integration suites, adding a dialect |

API documentation is also available through `go doc github.com/akfaiz/quill/db`.

## Status

Verified against PostgreSQL 16, MySQL 8.4, MariaDB 11.4 and SQLite (pure-Go driver) with
end-to-end tests, plus golden SQL tests for every dialect. Other server versions are untested.

Not supported yet: SQL Server, full-text and vector search,
per-group limits, query timeouts, and a conflict target for insert-or-ignore.
