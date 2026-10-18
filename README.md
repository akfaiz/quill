<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="logo-dark.svg">
    <img src="logo.svg" alt="quill logo" width="360">
  </picture>
</p>

<div align="center">

[![CI](https://github.com/akfaiz/quill/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/akfaiz/quill/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/akfaiz/quill/graph/badge.svg?token=bteQkNLky6)](https://codecov.io/gh/akfaiz/quill)
[![Go Reference](https://pkg.go.dev/badge/github.com/akfaiz/quill/db.svg)](https://pkg.go.dev/github.com/akfaiz/quill/db)
[![Go version](https://img.shields.io/github/go-mod/go-version/akfaiz/quill)](go.mod)
[![License: MIT](https://img.shields.io/github/license/akfaiz/quill)](LICENSE)

</div>

A fluent SQL query builder for Go, with an API inspired by Laravel's query builder. It builds SQL
with bind arguments and can optionally run it through `database/sql`. **It is not an ORM**: there
are no models, relations, model lifecycle hooks or migrations.

- Dialects: PostgreSQL, MySQL, MariaDB, SQLite
- Standard library only at runtime (testify and go-sqlmock are used by tests only)

## Features

**Building queries**

- One builder per statement (`Select`, `Insert`, `Update`, `Delete`), each exposing only the methods valid for it
- `SELECT` with aliases, `DISTINCT`, aggregates, `GROUP BY` / `HAVING`, ordering, limit and offset
- Rich `WHERE`: comparisons, `IN`, `BETWEEN`, `LIKE`, `NULL`, column and date-part comparisons,
  nested groups, `EXISTS`, row values, and `WhereAll` / `WhereAny` / `WhereNone`
- Joins (including subquery and lateral joins), unions, subqueries and common table expressions
  (`WITH`, `WITH RECURSIVE`)
- JSON predicates and JSON path updates, locks and index hints, raw SQL escape hatches
- Conditional building with `When` and `Tap`; `Clone` and snapshotting keep reused builders safe
- Inspect SQL with `ToSQL`, or with arguments interpolated through `ToRawSQL` (debugging only)

**Writing data**

- `Insert` (with `Ignore`, `Using` for insert-from-select and `Upsert`), `Update` (with `Set`,
  `Increment` / `Decrement` and `FromJoins`), `Delete`, truncate and `RETURNING`
- Struct writes with `Rows` / `SetRow` and `db:"name,omitempty"` tags

**Running queries** (optional: build-only use needs no connection)

- Scan into structs, maps and scalars; `First`, `Find`, `Sole`, `Value`, `Pluck`, `Exists`
- Offset, simple and cursor (keyset) pagination; `Chunk` and `Each` for large result sets
- Transactions, a query hook for logging, metrics and tracing, and a table prefix option
- Typed, select-only API with `From[T]`, so you get `[]T` back without a destination variable

## Why quill

- **You keep control of the SQL.** No models or relations to learn; every query reads like the SQL
  it produces.
- **One API, four dialects.** Quoting, placeholders and syntax differences are handled for you, and
  a feature a dialect cannot express returns `ErrUnsupported` instead of emitting SQL that
  behaves differently.
- **Safe by default.** Values are always bound, identifiers are always quoted, and an
  `UPDATE` or `DELETE` without a `WHERE` is refused unless you call `AllRows()`.
- **Errors, not panics.** A bad argument is recorded and returned from the terminal call. Only the
  `Must*` variants panic.
- **Fast to build.** Query construction is about 2 to 7 times faster than squirrel and goqu in
  the benchmark scenarios, with far fewer allocations; see the [benchmarks](test/benchmark/README.md).

## Installation

```sh
go get github.com/akfaiz/quill
```

```go
import "github.com/akfaiz/quill/db"
```

## Quick start

Build SQL without a database (set the dialect first, then start a statement with `Select`, `Insert`,
`Update` or `Delete`):

```go
sql, args, err := db.Use(db.Postgres).
    Select("id", "name").
    From("users").
    Where("votes", ">", 100).
    OrderByDesc("created_at").
    Limit(10).
    ToSQL()
// SELECT "id", "name" FROM "users" WHERE "votes" > $1 ORDER BY "created_at" DESC LIMIT 10
// args: [100]

sql, args, err = db.Use(db.Postgres).Update("users").Set("name", "Anna").Where("id", 7).ToSQL()
// UPDATE "users" SET "name" = $1 WHERE "id" = $2
// args: [Anna 7]
```

Run queries (wrap any `*sql.DB`, `*sql.Tx` or `*sql.Conn`). `db.Use` only builds SQL: running a
query on it returns `ErrNotBound`. `db.New` also executes:

```go
conn := db.New(sqlDB, db.Postgres)

var users []User
err := conn.Select().From("users").Where("active", true).OrderBy("id").Get(ctx, &users)

id, err := conn.Insert("users").Values(map[string]any{"name": "Ann"}).ExecGetID(ctx)
n, err  := conn.Update("users").Set("name", "Anna").Where("id", id).Exec(ctx)
```

> Name the variable `conn` (or anything but `db`): a local variable called `db` hides the
> package inside its scope.

Let a struct name its own table, and skip the destination variable (`db.From[T]` is select-only;
write structs with `Rows` and `SetRow`):

```go
type User struct {
    ID    int64   `db:"id,omitempty"`
    Name  string  `db:"name"`
    Email *string `db:"email"`
}

func (User) TableName() string { return "users" }

users, err := db.From[User](conn).Where("active", true).OrderBy("id").Get(ctx) // []User
id, err    := conn.Insert("users").Rows(User{Name: "Ann"}).ExecGetID(ctx)
_, err      = conn.Update("users").SetRow(User{Name: "Anna"}).Where("id", id).Exec(ctx)
```

Run several statements atomically:

```go
err := conn.Transaction(ctx, func(tx *db.DB) error {
    if _, err := tx.Update("accounts").Decrement("balance", 50).Where("id", 1).Exec(ctx); err != nil {
        return err
    }
    _, err := tx.Update("accounts").Increment("balance", 50).Where("id", 2).Exec(ctx)
    return err
})
```

## Documentation

Guides (getting started, building queries, writing data, running queries, the typed API, dialects,
security and the method reference) live in the [project wiki](https://github.com/akfaiz/quill/wiki).

| Resource | What it covers |
| --- | --- |
| [Wiki](https://github.com/akfaiz/quill/wiki) | User guides and method reference |
| [Benchmarks](test/benchmark/README.md) | Build-time comparison with go-sqlbuilder, goqu and squirrel |
| [Contributing](CONTRIBUTING.md) | Layout, tests, fuzzing, integration suites, adding a dialect |

## License

MIT License. See [LICENSE](LICENSE) for details.
