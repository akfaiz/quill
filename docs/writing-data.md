# Writing data

Write methods run a statement and return the number of rows affected (`int64`). Each also has a
build-only `...SQL` form that returns the statement instead of running it, so the same code works
with `db.Use(...)`. Struct-based writes are in the [Typed API](typed-api.md).

- [Insert](#insert) · [Upsert](#upsert) · [Update](#update) · [Increment and decrement](#increment-and-decrement)
- [Delete](#delete) · [Truncate](#truncate) · [RETURNING](#returning) · [The WHERE guard](#the-where-guard)
- [Updating JSON paths](#updating-json-paths) · [Build-only forms](#build-only-forms)

## Insert

```go
n, err := conn.Table("users").Insert(ctx,
    map[string]any{"name": "Eve", "email": "eve@example.com"},
    map[string]any{"name": "Fay", "email": "fay@example.com"},
)
// INSERT INTO "users" ("email", "name") VALUES ($1, $2), ($3, $4)
```

- Columns are the **sorted** keys of the first row, so the SQL is deterministic. Every row must
  have exactly the same keys, otherwise you get `db.ErrInvalidArgs`.
- No rows is a successful no-op (`0, nil`). One empty map inserts default values
  (`INSERT INTO "t" DEFAULT VALUES`; `() VALUES ()` on MySQL).
- A value may be `db.Raw("now()")` (inlined) or a `*db.Builder` (a scalar subquery).

### Insert and get the id

```go
id, err := conn.Table("users").InsertGetID(ctx, map[string]any{"name": "Gus"})
id, err  = conn.Table("users").InsertGetID(ctx, row, "user_id") // PostgreSQL: a different id column
```

PostgreSQL appends `RETURNING "id"` (or your column); MySQL and SQLite read the driver's last
insert id. The id must be an integer.

### Insert and ignore conflicts

```go
n, err := conn.Table("users").InsertOrIgnore(ctx, map[string]any{"email": "ann@example.com"})
```

| | SQL |
| --- | --- |
| PostgreSQL | `INSERT ... ON CONFLICT DO NOTHING` |
| MySQL | `INSERT IGNORE INTO ...` |
| SQLite | `INSERT OR IGNORE INTO ...` |

The count is the rows actually inserted, so a skipped conflict reports `0`.

### Insert from a query

```go
n, err := conn.Table("archive").InsertUsing(ctx, []string{"id", "name"},
    conn.Table("users").Select("id", "name").Where("active", false))
// INSERT INTO "archive" ("id", "name") SELECT "id", "name" FROM "users" WHERE "active" = $1
```

An empty column list, or `[]string{"*"}`, omits the column list. `InsertOrIgnoreUsing` skips
conflicts.

## Upsert

```go
rows := []map[string]any{
    {"email": "ann@example.com", "name": "Ann"},
    {"email": "bob@example.com", "name": "Bob"},
}
n, err := conn.Table("users").Upsert(ctx, rows, []string{"email"}, []string{"name"})
```

| Argument | Meaning |
| --- | --- |
| `rows` | As for `Insert` |
| `uniqueBy` | The conflict target. Required on PostgreSQL and SQLite, ignored on MySQL |
| `update` | The columns that take the inserted value on conflict. `nil` means every inserted column; an empty non-nil slice is an error |

PostgreSQL and SQLite render `ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name"`;
MySQL renders ``ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)``. **MySQL reports 2 affected rows
for each row it updated**, not 1.

## Update

```go
n, err := conn.Table("users").Where("id", 7).Update(ctx, map[string]any{
    "name": "Anna",
    "age":  31,
})
// UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3
```

- Columns are sorted. Values may be `db.Raw(...)` or a subquery.
- Placeholders are numbered in statement order: the values first, then the `WHERE` arguments.
- `Update` without a `WHERE` is refused; see [the guard](#the-where-guard).

### With joins or a limit

```go
conn.Table("users as u").
    Join("orders as o", "o.user_id", "u.id").
    Where("o.total", ">", 100).
    Update(ctx, map[string]any{"u.vip": true})

conn.Table("jobs").Where("state", "queued").OrderBy("id").Limit(10).Update(ctx, map[string]any{"state": "taken"})
```

MySQL runs these natively (`UPDATE ... JOIN ... SET`, and `ORDER BY ... LIMIT` on a single table).
PostgreSQL and SQLite rewrite them to `WHERE "ctid" IN (SELECT ...)` and `WHERE "rowid" IN (SELECT ...)`,
so the join conditions and the limit still apply. On those two, qualified `SET` columns such as
`"u.vip"` lose their qualifier.

### Update from another table (PostgreSQL)

```go
conn.Table("users").
    Join("accounts", "accounts.user_id", "users.id").
    Where("accounts.active", true).
    UpdateFrom(ctx, map[string]any{"plan": db.Raw(`"accounts"."plan"`)})
// UPDATE "users" SET "plan" = "accounts"."plan" FROM "accounts" WHERE "accounts"."active" = $1 AND "accounts"."user_id" = "users"."id"
```

The join conditions become part of the `WHERE`. Other dialects return `db.ErrUnsupported`.

### Update or insert

```go
ok, err := conn.Table("users").UpdateOrInsert(ctx,
    map[string]any{"email": "zed@example.com"},   // attributes to match
    map[string]any{"name": "Zed"},                // values to set
)
```

Updates the first row matching the attributes, or inserts attributes plus values. It is two
statements and **not atomic**: wrap it in a transaction and keep a unique index if concurrent
writers matter.

## Increment and decrement

```go
n, err := conn.Table("posts").Where("id", 3).Increment(ctx, "views", 1, nil)
n, err  = conn.Table("posts").Where("id", 3).Decrement(ctx, "stock", 2, map[string]any{"updated_by": 9})
n, err  = conn.Table("posts").Where("id", 3).IncrementEach(ctx, map[string]any{"views": 1, "likes": 5}, nil)
// UPDATE "posts" SET "views" = "views" + $1 WHERE "id" = $2
```

The amount is bound, not inlined, and must be a number (anything else is `db.ErrInvalidArgs`).
The last argument sets extra columns in the same statement; an extra column wins over an
incremented column of the same name. `DecrementEach` is the counterpart.

## Delete

```go
n, err := conn.Table("sessions").Where("expires_at", "<", now).Delete(ctx)
```

Joins and limits work like `Update`: native on MySQL (`DELETE alias FROM ... JOIN ...`, plus
`ORDER BY ... LIMIT`), and a `ctid`/`rowid` subquery on PostgreSQL and SQLite.

```go
conn.Table("posts as p").Join("users as u", "u.id", "p.user_id").Where("u.banned", true).Delete(ctx)
```

## Truncate

```go
err := conn.Table("events").Truncate(ctx)
```

| | Statement |
| --- | --- |
| PostgreSQL | `TRUNCATE "events" RESTART IDENTITY` (no `CASCADE`: tables that reference it are never emptied implicitly) |
| MySQL | ``TRUNCATE TABLE `events` `` |
| SQLite | `DELETE FROM sqlite_sequence WHERE name = ?`, then `DELETE FROM "events"` |

`Truncate` is the explicit way to empty a table; an unbounded `Delete` is refused.

## RETURNING

```go
type Row struct {
    ID   int64
    Name string
}

var out []Row
err := conn.Table("users").Returning("id", "name").
    InsertReturning(ctx, &out, map[string]any{"name": "Rae"})

err = conn.Table("users").Where("id", 7).Returning("id", "name").UpdateReturning(ctx, &out, map[string]any{"name": "Rae2"})
err = conn.Table("users").Where("id", 7).Returning("*").DeleteReturning(ctx, &out)
```

`Returning` works on PostgreSQL and SQLite 3.35+; MySQL and MariaDB return `db.ErrUnsupported`. The destination is a pointer to a slice, scanned like `Get`. Calling the
`...Returning` methods without `Returning(...)` is `db.ErrInvalidArgs`. With the
[typed API](typed-api.md#returning-typed-rows) the columns default to `*` and you get `[]T` back.

## The WHERE guard

`Update`, `Delete`, `Increment`, `Decrement`, `IncrementEach` and `DecrementEach` return
`db.ErrNoWhere` when the builder has no `WHERE` clause and no `LIMIT`. This stops a forgotten
filter from rewriting or deleting a whole table.

```go
_, err := conn.Table("users").Update(ctx, map[string]any{"active": false})
// db.ErrNoWhere

n, err := conn.Table("users").AllRows().Update(ctx, map[string]any{"active": false}) // confirmed
```

`AllRows()` states that every row is meant. A `Limit(...)` also counts as a bound. `UpdateFrom`
accepts joins in place of a `WHERE`.

## Updating JSON paths

Use `column->path` as the key:

```go
conn.Table("users").Where("id", 1).Update(ctx, map[string]any{
    "options->theme":  "dark",
    "options->tags[0]": "go",
    "name":            "Ann",
})
```

| | SQL |
| --- | --- |
| PostgreSQL | `"options" = JSONB_SET("options"::JSONB, '{"theme"}', $1)` (values are JSON-encoded) |
| MySQL | `` `options` = JSON_SET(`options`, '$."theme"', ?) `` (booleans inlined; maps and slices bound as `CAST(? AS JSON)`) |
| SQLite | one `JSON_PATCH(IFNULL("options", JSON('{}')), JSON(?))` per column, merging every path of that column |

Several paths of the same column become **one assignment** (nested `JSONB_SET` on PostgreSQL, a
multi-pair `JSON_SET` on MySQL), because PostgreSQL rejects assigning a column twice.

Limits: on PostgreSQL, MySQL and MariaDB only the last key is created, so write `options->a->b` only if
`options->a` already exists (SQLite's merge patch creates intermediate objects). SQLite does not
support `db.Raw(...)` as a JSON value and returns `db.ErrUnsupported`.

## Build-only forms

Each write has an SQL form that runs nothing, for use with `db.Use(...)`:

| Run | Build only |
| --- | --- |
| `Insert` | `InsertSQL(rows...)` |
| `InsertOrIgnore` | `InsertOrIgnoreSQL(rows...)` |
| `InsertGetID` | `InsertGetIDSQL(row, sequence...)` |
| `InsertUsing`, `InsertOrIgnoreUsing` | `InsertUsingSQL`, `InsertOrIgnoreUsingSQL` |
| `Upsert` | `UpsertSQL(rows, uniqueBy, update)` |
| `Update`, `UpdateFrom` | `UpdateSQL(values)`, `UpdateFromSQL(values)` |
| `Increment`, `Decrement`, `...Each` | `IncrementSQL`, `DecrementSQL`, `IncrementEachSQL`, `DecrementEachSQL` |
| `Delete` | `DeleteSQL()` |
| `Truncate` | `TruncateSQL()` returns `[]db.Stmt` (SQLite needs two statements) |

```go
sql, args, err := db.Use(db.Postgres).Table("users").Where("id", 7).UpdateSQL(map[string]any{"name": "Anna"})
```

They apply the same rules, including the `WHERE` guard and `Returning(...)`.
