# Writing data

Each write statement has its own builder: `conn.Insert("t")` returns an `*db.InsertBuilder`,
`conn.Update("t")` an `*db.UpdateBuilder` and `conn.Delete("t")` a `*db.DeleteBuilder`. `Exec(ctx)`
runs the statement and returns the number of rows affected (`int64`); `ToSQL()` returns the statement
instead of running it, so the same code works with `db.Use(...)`. Truncate and update-or-insert are
methods on `*db.DB`. Struct-based writes use `Rows(...)` and `SetRow(...)` (see
[Struct writes](typed-api.md#struct-writes)).

Each builder offers only what is valid for its statement: an insert has no `Where`, `Join`, `OrderBy`
or `Limit`; an update or delete accepts `Where*`, `Join*`, `OrderBy*`, `Limit`, `Returning` and
`AllRows`, but not `GroupBy`, `Having`, `Union`, locks, `With` or `Offset`.

- [Insert](#insert) · [Upsert](#upsert) · [Update](#update) · [Increment and decrement](#increment-and-decrement)
- [Delete](#delete) · [Truncate](#truncate) · [RETURNING](#returning) · [The WHERE guard](#the-where-guard)
- [Updating JSON paths](#updating-json-paths) · [Build-only forms](#build-only-forms)

## Insert

```go
n, err := conn.Insert("users").
    Values(
        map[string]any{"name": "Eve", "email": "eve@example.com"},
        map[string]any{"name": "Fay", "email": "fay@example.com"},
    ).
    Exec(ctx)
// INSERT INTO "users" ("email", "name") VALUES ($1, $2), ($3, $4)
```

- Columns are the **sorted** keys of the first row, so the SQL is deterministic. Every row must
  have exactly the same keys, otherwise you get `db.ErrInvalidArgs`.
- Structs work too: `Rows(User{...}, User{...})` takes the columns from the `db` tags (see
  [Struct writes](typed-api.md#struct-writes)). `Values` and `Rows` append.
- No rows is a successful no-op for `Exec` (`0, nil`); `ToSQL` returns `db.ErrNoValues`. One empty map inserts default values
  (`INSERT INTO "t" DEFAULT VALUES`; `() VALUES ()` on MySQL).
- A value may be `db.Raw("now()")` (inlined) or a `*db.SelectBuilder` (a scalar subquery).

### Insert and get the id

```go
id, err := conn.Insert("users").Values(map[string]any{"name": "Gus"}).ExecGetID(ctx)
id, err  = conn.Insert("users").Values(row).ExecGetID(ctx, "user_id") // PostgreSQL: a different id column
```

PostgreSQL appends `RETURNING "id"` (or your column); MySQL and SQLite read the driver's last
insert id. The id must be an integer.

### Insert and ignore conflicts

```go
n, err := conn.Insert("users").Values(map[string]any{"email": "ann@example.com"}).Ignore().Exec(ctx)
```

| | SQL |
| --- | --- |
| PostgreSQL | `INSERT ... ON CONFLICT DO NOTHING` |
| MySQL | `INSERT IGNORE INTO ...` |
| SQLite | `INSERT OR IGNORE INTO ...` |

The count is the rows actually inserted, so a skipped conflict reports `0`.

### Insert from a query

```go
n, err := conn.Insert("archive").
    Using([]string{"id", "name"}, conn.Select("id", "name").From("users").Where("active", false)).
    Exec(ctx)
// INSERT INTO "archive" ("id", "name") SELECT "id", "name" FROM "users" WHERE "active" = $1
```

An empty column list, or `[]string{"*"}`, omits the column list. Add `.Ignore()` to skip
conflicts. `With` on the insert builder moves its CTEs into the select (see
[Common table expressions](building-queries.md#common-table-expressions)).

## Upsert

```go
rows := []map[string]any{
    {"email": "ann@example.com", "name": "Ann"},
    {"email": "bob@example.com", "name": "Bob"},
}
n, err := conn.Insert("users").Values(rows...).Upsert([]string{"email"}, []string{"name"}).Exec(ctx)
```

| Argument | Meaning |
| --- | --- |
| rows | Set with `Values` or `Rows`, as for any insert |
| `uniqueBy` | The conflict target. Required on PostgreSQL and SQLite, ignored on MySQL |
| `update` | The columns that take the inserted value on conflict. `nil` means every inserted column; an empty non-nil slice is an error |

PostgreSQL and SQLite render `ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"`;
MySQL renders ``ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)``. **MySQL reports 2 affected rows
for each row it updated**, not 1.

## Update

```go
n, err := conn.Update("users").
    SetMap(map[string]any{"name": "Anna", "age": 31}).
    Where("id", 7).
    Exec(ctx)
// UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3

n, err = conn.Update("users").Set("name", "Anna").Set("age", 31).Where("id", 7).Exec(ctx)
// UPDATE "users" SET "name" = $1, "age" = $2 WHERE "id" = $3
```

- `SetMap` sorts the columns; `Set` keeps the order you call it in, and setting a column again
  replaces its value. Values may be `db.Raw(...)` or a `*db.SelectBuilder` subquery. `SetRow(struct)`
  takes the columns from a struct (see [Struct writes](typed-api.md#struct-writes)).
- Placeholders are numbered in statement order: the values first, then the `WHERE` arguments.
- `Exec` and `ToSQL` without a `WHERE` or `LIMIT` are refused; see [the guard](#the-where-guard).
- An update with nothing to set is `db.ErrNoValues`.

### With joins or a limit

```go
conn.Update("users as u").
    Join("orders as o", "o.user_id", "u.id").
    Where("o.total", ">", 100).
    Set("u.vip", true).
    Exec(ctx)

conn.Update("jobs").Where("state", "queued").OrderBy("id").Limit(10).Set("state", "taken").Exec(ctx)
```

MySQL runs these natively (`UPDATE ... JOIN ... SET`, and `ORDER BY ... LIMIT` on a single table).
MySQL and MariaDB do not accept `ORDER BY` or `LIMIT` on a write with joins, so that combination
returns `ErrUnsupported` there instead of dropping the limit and touching every joined row.
PostgreSQL and SQLite rewrite them to `WHERE "ctid" IN (SELECT ...)` and `WHERE "rowid" IN (SELECT ...)`,
so the join conditions and the limit still apply. On those two, qualified `SET` columns such as
`"u.vip"` lose their qualifier.

### Update from another table (PostgreSQL)

```go
conn.Update("users").
    Join("accounts", "accounts.user_id", "users.id").
    FromJoins().
    Where("accounts.active", true).
    Set("plan", db.Raw(`"accounts"."plan"`)).
    Exec(ctx)
// UPDATE "users" SET "plan" = "accounts"."plan" FROM "accounts" WHERE "accounts"."active" = $1 AND "accounts"."user_id" = "users"."id"
```

`FromJoins()` makes the joined tables the `FROM` list, and the join conditions become part of the
`WHERE`. Other dialects return `db.ErrUnsupported` (MySQL takes `Join` directly, as above).

### Update or insert

```go
ok, err := conn.UpdateOrInsert(ctx, "users",
    map[string]any{"email": "zed@example.com"},   // attributes to match
    map[string]any{"name": "Zed"},                // values to set
)
```

A method on `*db.DB`: updates the first row matching the attributes, or inserts attributes plus
values (`ok` reports whether the write succeeded). It is two
statements and **not atomic**: wrap it in a transaction and keep a unique index if concurrent
writers matter.

## Increment and decrement

```go
n, err := conn.Update("posts").Increment("views", 1).Where("id", 3).Exec(ctx)
n, err  = conn.Update("posts").Decrement("stock", 2).SetMap(map[string]any{"updated_by": 9}).Where("id", 3).Exec(ctx)
n, err  = conn.Update("posts").IncrementEach(map[string]any{"views": 1, "likes": 5}).Where("id", 3).Exec(ctx)
// UPDATE "posts" SET "views" = "views" + $1 WHERE "id" = $2
```

The amount is bound, not inlined, and must be a number (anything else is `db.ErrInvalidArgs`).
Chain `Set` or `SetMap` to set extra columns in the same statement. `DecrementEach` is the
counterpart of `IncrementEach`.

## Delete

```go
n, err := conn.Delete("sessions").Where("expires_at", "<", now).Exec(ctx)
```

Joins and limits work like `Update`: native on MySQL (`DELETE alias FROM ... JOIN ...`, or
`ORDER BY ... LIMIT` on a single table; both together return `ErrUnsupported`), and a `ctid`/`rowid`
subquery on PostgreSQL and SQLite.

```go
conn.Delete("posts as p").Join("users as u", "u.id", "p.user_id").Where("u.banned", true).Exec(ctx)
```

## Truncate

```go
err := conn.Truncate(ctx, "events")
```

| | Statement |
| --- | --- |
| PostgreSQL | `TRUNCATE "events" RESTART IDENTITY` (no `CASCADE`: tables that reference it are never emptied implicitly) |
| MySQL | ``TRUNCATE TABLE `events` `` |
| SQLite | `DELETE FROM sqlite_sequence WHERE name = ?`, then `DELETE FROM "events"` |

`Truncate` is a method on `*db.DB` and the explicit way to empty a table; an unbounded `Delete` is
refused.

## RETURNING

```go
type Row struct {
    ID   int64
    Name string
}

var out []Row
err := conn.Insert("users").Values(map[string]any{"name": "Rae"}).Returning("id", "name").Get(ctx, &out)

err = conn.Update("users").Set("name", "Rae2").Where("id", 7).Returning("id", "name").Get(ctx, &out)
err = conn.Delete("users").Where("id", 7).Returning("*").Get(ctx, &out)
```

`Returning` works on PostgreSQL and SQLite 3.35+; MySQL and MariaDB return `db.ErrUnsupported`. The destination is a pointer to a slice, scanned like a select's `Get`. On a write builder,
`Get(ctx, &dest)` runs the statement and scans the `RETURNING` rows; calling it without
`Returning(...)` is `db.ErrInvalidArgs`. `Exec` ignores the returned rows and gives the affected
count.

## The WHERE guard

An `UpdateBuilder` (including `Increment`, `Decrement`, `IncrementEach` and `DecrementEach`) and a
`DeleteBuilder` return `db.ErrNoWhere` from `Exec`, `Get` and `ToSQL` when the builder has no `WHERE`
clause and no `LIMIT`. This stops a forgotten
filter from rewriting or deleting a whole table.

```go
_, err := conn.Update("users").Set("active", false).Exec(ctx)
// db.ErrNoWhere

n, err := conn.Update("users").Set("active", false).AllRows().Exec(ctx) // confirmed
```

`AllRows()` states that every row is meant. A `Limit(...)` also counts as a bound. An update with
`FromJoins()` accepts joins in place of a `WHERE`.

## Updating JSON paths

Use `column->path` as the key:

```go
conn.Update("users").
    Set("options->theme", "dark").
    Set("options->tags[0]", "go").
    Set("name", "Ann").
    Where("id", 1).
    Exec(ctx)
```

| | SQL |
| --- | --- |
| PostgreSQL | `"options" = JSONB_SET("options"::JSONB, '{"theme"}', $1)` (values are JSON-encoded) |
| MySQL | `` `options` = JSON_SET(`options`, '$."theme"', ?) `` (booleans inlined; maps and slices bound as `CAST(? AS JSON)`) |
| SQLite | `"options" = JSON_SET(IFNULL("options", JSON('{}')), '$."theme"', JSON(?))` (values are JSON-encoded) |

Every dialect sets the value at the path: `nil` becomes JSON `null` (the key stays), and a map or slice
replaces whatever was there instead of merging into it.

Several paths of the same column become **one assignment** (nested `JSONB_SET` on PostgreSQL, a
multi-pair `JSON_SET` on MySQL and SQLite), because PostgreSQL rejects assigning a column twice.

Limits: on PostgreSQL, MySQL and MariaDB only the last key is created, so write `options->a->b` only if
`options->a` already exists. SQLite 3.53.4 creates missing intermediate objects too; that is not
verified on older SQLite versions, so do not rely on it. SQLite does not support `db.Raw(...)` as a JSON
value and returns `db.ErrUnsupported`.

## Build-only forms

Every write builder ends in `ToSQL()` instead of `Exec`, for use with `db.Use(...)` (or to log or
test a statement). The `...SQL` methods of the old single builder are gone.

| Run | Build only |
| --- | --- |
| `Insert(...).Exec(ctx)` | `Insert(...).ToSQL()` |
| `Insert(...).ExecGetID(ctx, seq...)` | `Insert(...).IDSQL(seq...)` |
| `Update(...).Exec(ctx)` (including `Increment`, `FromJoins`) | `Update(...).ToSQL()` |
| `Delete(...).Exec(ctx)` | `Delete(...).ToSQL()` |
| `conn.Truncate(ctx, table)` | `conn.TruncateSQL(table)` returns `[]db.Stmt` (SQLite needs two statements) |

```go
sql, args, err := db.Use(db.Postgres).Update("users").Set("name", "Anna").Where("id", 7).ToSQL()
```

They apply the same rules, including the `WHERE` guard and `Returning(...)`.
