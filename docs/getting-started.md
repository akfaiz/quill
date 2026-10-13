# Getting started

## Install

```sh
go get github.com/akfaiz/quill
```

```go
import "github.com/akfaiz/quill/db"
```

The library has no third-party dependencies. You bring your own `database/sql` driver.

## Two ways in

| | Function | Needs a connection | Use it to |
| --- | --- | --- | --- |
| Build only | `db.Use(dialect, opts...)` | no | produce `(sql, args)` for your own driver, tests, logging |
| Build and run | `db.New(conn, dialect, opts...)` | yes | build and execute |

Both return a `*db.DB`. A statement starts from it: `Select(...).From("name")`, `Insert("name")`,
`Update("name")` or `Delete("name")`. Each returns its own builder, so a builder only offers the methods
that are valid for that statement.

### Build only

```go
pg := db.Use(db.Postgres)          // set the dialect once, reuse it

sql, args, err := pg.Select().From("users").Where("id", 7).ToSQL()
sql, args, err  = pg.Select().From("posts").Limit(5).ToSQL()

// one-liner
sql, args, err = db.Use(db.MySQL).Select().From("users").Limit(5).ToSQL()
```

Running a query on a build-only `DB` returns `db.ErrNotBound`; it never panics.

### Build and run

`db.New` accepts anything with `QueryContext` and `ExecContext` (the `db.Conn` interface), which
is satisfied by `*sql.DB`, `*sql.Tx` and `*sql.Conn`.

```go
import (
    "database/sql"

    "github.com/akfaiz/quill/db"
    _ "github.com/jackc/pgx/v5/stdlib" // any database/sql driver
)

sqlDB, err := sql.Open("pgx", dsn)
if err != nil { /* ... */ }

conn := db.New(sqlDB, db.Postgres)
```

Dialect constants: `db.Postgres`, `db.MySQL`, `db.MariaDB`, `db.SQLite`. The dialect decides quoting
(`"x"` or `` `x` ``), placeholders (`$1` or `?`) and every dialect-specific clause. See
[Dialects](dialects.md).

> Call the variable `conn`, not `db`: a variable named `db` hides the package.

### Options

Both constructors take the same options.

| Option | Effect |
| --- | --- |
| `db.WithPrefix("app_")` | Prefixes table names, aliases and qualified columns: `From("users as u")` becomes `"app_users" as "app_u"` |
| `db.WithHook(fn)` | Calls `fn(ctx, db.QueryEvent)` after every statement (logging, metrics, tracing) |
| `db.WithLogArgs()` | Includes bind values in `QueryEvent.Args`. Off by default, because arguments can be personal data |

```go
conn := db.New(sqlDB, db.Postgres,
    db.WithPrefix("app_"),
    db.WithHook(func(ctx context.Context, e db.QueryEvent) {
        log.Printf("%s args=%d took=%s err=%v", e.SQL, e.NumArgs, e.Duration, e.Err)
    }),
)
```

## Your first queries

```go
ctx := context.Background()

type User struct {
    ID     int64
    Name   string
    Email  *string // pointer: NULL becomes nil
    Active bool
}

// read
var users []User
err := conn.Select().From("users").Where("active", true).OrderBy("id").Get(ctx, &users)

var u User
err = conn.Select().From("users").Find(ctx, 7, &u) // errors.Is(err, db.ErrNotFound) when missing

// write
id, err := conn.Insert("users").Values(map[string]any{"name": "Ann"}).ExecGetID(ctx)
n, err  := conn.Update("users").Set("name", "Anna").Where("id", id).Exec(ctx)
n, err   = conn.Delete("users").Where("id", id).Exec(ctx)
```

Every method that touches the database takes a `context.Context` first and honours
cancellation.

## How a builder behaves

- **One builder per statement.** `Select` returns a `*db.SelectBuilder`, `Insert` an
  `*db.InsertBuilder`, `Update` an `*db.UpdateBuilder` and `Delete` a `*db.DeleteBuilder`. Select-only
  clauses (`GroupBy`, `Having`, `Union`, locks, `With`, `Offset`, pagination) exist only on the
  select builder; an insert has no `Where`.
- **Fluent and mutable.** Each call changes the builder and returns it. Use `Clone()` to branch.
- **Errors are collected.** A bad operator, direction or argument is recorded on the builder, the
  chain keeps working, and the first error comes back from the terminal call (`ToSQL`, `Get`,
  `Exec`, ...). `Err()` shows it early.
- **Subqueries are snapshots.** A builder you pass as a subquery is copied when it is added, so
  changing it later does not change the outer query.
- **Not safe for concurrent use.** Build per request; `Clone()` to share a base query.

## Must variants

For fixed queries (tests, package-level variables) where a failure is a programming mistake:

```go
sql, args := db.Use(db.Postgres).Select().From("users").Where("id", 7).MustSQL()
raw       := db.Use(db.Postgres).Select().From("users").Where("name", "ann").MustRawSQL() // debug only
```

They panic with the original error, wrapped, so `errors.Is` still works on the recovered value.
Do not use them where the error can come from input (a user-chosen operator or column).

## Errors

All errors are sentinel values you can test with `errors.Is`. The full list is in the
[reference](reference.md#errors). The ones you will meet first:

| Error | Meaning |
| --- | --- |
| `db.ErrNotFound` | `First`, `Find`, `Sole` or `Value` matched no row (it is `sql.ErrNoRows`) |
| `db.ErrNoWhere` | An update or delete had no `WHERE` or `LIMIT`; see [the guard](writing-data.md#the-where-guard) |
| `db.ErrInvalidOperator` | An operator outside the allow-list |
| `db.ErrNotBound` | You ran a query on a build-only `DB` |
| `db.ErrUnsupported` | The feature does not exist on this dialect |

## Next

[Building queries](building-queries.md), then [Running queries](running-queries.md).
