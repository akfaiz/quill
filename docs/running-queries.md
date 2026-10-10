# Running queries

These methods need a `DB` created with `db.New(conn, dialect)`. All take a `context.Context`
first, honour cancellation, and always close their rows.

- [Reading rows](#reading-rows) · [How rows are scanned](#how-rows-are-scanned) · [Aggregates](#aggregates)
- [Pagination](#pagination) · [Chunking and streaming](#chunking-and-streaming)
- [Transactions](#transactions) · [Hooks and logging](#hooks-and-logging) · [Raw statements](#raw-statements)

## Reading rows

The destination is a pointer; its type decides how rows are read (see
[scanning](#how-rows-are-scanned)).

```go
var users []User
err := conn.Table("users").Where("active", true).OrderBy("id").Get(ctx, &users)   // every row

var u User
err = conn.Table("users").Where("email", e).First(ctx, &u)   // first row; db.ErrNotFound when none
err = conn.Table("users").Find(ctx, 7, &u)                   // WHERE "id" = 7, then First
err = conn.Table("users").Where("email", e).Sole(ctx, &u)    // exactly one: ErrNotFound or ErrMultipleRecords
```

| Method | Destination | Result |
| --- | --- | --- |
| `Get(ctx, &dest)` | `*[]T` | every row |
| `First(ctx, &dest)` | `*T` | first row, `db.ErrNotFound` if none |
| `Find(ctx, id, &dest)` | `*T` | the row whose `id` is `id` |
| `Sole(ctx, &dest)` | `*T` | the only row; `db.ErrMultipleRecords` for several |
| `Value(ctx, "col", &dest)` | `*scalar` | one column of the first row |
| `Pluck(ctx, "col", &dest)` | `*[]scalar` | one column of every row |
| `PluckMap(ctx, "col", "key", &dest)` | `*map[K]V` | `key -> col` |
| `Exists(ctx)` / `DoesntExist(ctx)` | | `bool` |

```go
var name string
err := conn.Table("users").Where("id", 2).Value(ctx, "name", &name)

var names []string
err = conn.Table("users").OrderBy("name").Pluck(ctx, "name", &names)

var byID map[int64]string
err = conn.Table("users").PluckMap(ctx, "name", "id", &byID)
```

`First`, `Find`, `Sole` and `Value` add `LIMIT 1` (or 2 for `Sole`) on a copy; your builder is
unchanged.

## How rows are scanned

**Destination types**

| Element type | Result |
| --- | --- |
| struct, `*struct` | columns map onto fields |
| `map[string]any` | one entry per column; `[]byte` becomes `string`, `NULL` stays `nil` |
| scalar (`int`, `string`, `time.Time`, ...) | the query must return exactly one column |

An empty result gives an empty, non-nil slice.

**Struct fields**

- A field matches its `db:"name"` tag, otherwise the snake_case of its name (`UserName` is
  `user_name`, `HTTPServer` is `http_server`). Matching falls back to case-insensitive.
- `db:"-"` ignores a field. Unexported fields are ignored. Columns with no field are ignored.
- Embedded structs are flattened, and an outer field wins over an embedded one of the same name.
  An embedded pointer to an unexported type cannot be set by reflection, so its columns are
  discarded.
- `sql.Scanner` types (`sql.NullString`, `time.Time`, your own) are scanned as one value.

**NULL**

- Into a pointer or a `sql.Null*` field: you can tell `NULL` apart.
- Into a plain field (`string`, `int`, `bool`, ...): it leaves the **zero value** instead of
  failing, so a nullable column never aborts a scan.

```go
type User struct {
    ID    int64
    Email *string        // nil when NULL
    Bio   string         // "" when NULL
    Nick  sql.NullString // Valid=false when NULL
}
```

## Aggregates

```go
n, err   := conn.Table("users").Count(ctx)                     // count(*)
n, err    = conn.Table("users").Count(ctx, "age")               // count("age"): skips NULLs
sum, err := conn.Table("orders").Sum(ctx, "total")              // float64, 0 when no rows
avg, err := conn.Table("orders").Avg(ctx, "total")

var oldest time.Time
err = conn.Table("users").Min(ctx, "created_at", &oldest)       // Max likewise
err = conn.Table("orders").Aggregate(ctx, "sum", "total", &f)   // any function
```

Ordering is dropped from the aggregate. Queries with `GROUP BY`, `HAVING`, `UNION`, `DISTINCT`,
`LIMIT` or `OFFSET` are wrapped so the aggregate covers the final row set:

```sql
SELECT count(*) AS "aggregate" FROM (SELECT "user_id" FROM "posts" GROUP BY "user_id") AS "aggregate_table"
```

Strict servers (PostgreSQL, MySQL with `ONLY_FULL_GROUP_BY`) need the grouped column selected:
`Select("user_id").GroupBy("user_id").Count(ctx)`. `Min`, `Max` and `Sum` over no rows give `NULL`,
which leaves a plain destination at its zero value.

## Pagination

### Length-aware

```go
var users []User
page, err := conn.Table("users").OrderBy("id").Paginate(ctx, 2, 15, &users)
// page.Total, page.PerPage, page.CurrentPage, page.LastPage, page.From, page.To
```

`Paginate(ctx, page, perPage, &dest)` runs a count (ignoring order, limit and offset) and then
fetches the page. `page` is 1-based and below 1 becomes 1; `perPage` must be at least 1. An empty
table gives `Total: 0, LastPage: 1` and an empty slice.

### Without a count

```go
p, err := conn.Table("users").OrderBy("id").SimplePaginate(ctx, 1, 15, &users)
// p.HasMore, p.CurrentPage, p.PerPage
```

Fetches one extra row to learn whether another page exists, and skips the count query.

### Keyset (cursor)

```go
var users []User
p, err := conn.Table("users").CursorPaginate(ctx, 15, "", &users)             // first page
p, err  = conn.Table("users").CursorPaginate(ctx, 15, p.NextCursor, &users)   // next page
// p.HasMore, p.NextCursor ("" when there is no next page)

conn.Table("events").CursorPaginate(ctx, 50, token, &events, db.CursorOptions{Column: "created_at", Desc: true})
```

- Pages by `WHERE key > last ORDER BY key LIMIT n`, so it stays fast on deep pages and is stable
  while rows are inserted.
- The key column (default `"id"`) must be **unique and sortable**; ties would skip or repeat rows.
  It replaces any existing `ORDER BY`.
- The cursor is an opaque, URL-safe token. Its value is only ever bound as an argument, so a
  tampered token cannot change the SQL (see [Security](security.md#cursors)). It is **not**
  encrypted or signed and reveals the last key.
- A `NULL` key cannot be a cursor; the next request is rejected with `db.ErrInvalidArgs`.

## Chunking and streaming

### Chunk

```go
var batch []User
err := conn.Table("users").OrderBy("id").Chunk(ctx, 500, &batch, func() (bool, error) {
    for _, u := range batch {
        // process u
    }
    return true, nil // false stops early; an error stops and is returned
})
```

`Chunk` pages by offset, so it needs an `ORDER BY` (`db.ErrNoOrder` otherwise). If the callback
changes the rows that match the filter, offset paging can skip or repeat rows: use `ChunkByID`.

### ChunkByID

```go
err := conn.Table("jobs").Where("state", "new").ChunkByID(ctx, 100, &batch, func() (bool, error) {
    // safe to update or delete the rows in batch
    return true, nil
}) // keyset on "id"; pass a column name last to use another unique column
```

The next page starts after the last key as fetched, read before `fn` runs, so `fn` may sort,
filter or truncate `batch` without changing which rows come next.

### One row at a time

```go
var row User
err := conn.Table("users").OrderBy("id").Each(ctx, &row, func() error {
    // row holds the current row, reset before every scan
    return nil
})

for u, err := range db.Stream[User](ctx, conn.Table("users").OrderBy("id")) {
    if err != nil { return err }
    if u.Name == "stop" { break } // breaking closes the cursor
}
```

Both hold a single row in memory. `Stream` is a Go iterator (`iter.Seq2[T, error]`); an error ends
the sequence after being yielded once.

> With a single-connection pool (such as an in-memory SQLite database), do not run another query
> from inside an `Each` callback or a `Stream` loop: the open cursor holds the connection.

## Transactions

```go
err := conn.Transaction(ctx, func(tx *db.DB) error {
    if _, err := tx.Table("accounts").Where("id", 1).Decrement(ctx, "balance", 50, nil); err != nil {
        return err // rolls back
    }
    _, err := tx.Table("accounts").Where("id", 2).Increment(ctx, "balance", 50, nil)
    return err // nil commits
})
```

- Commit on `nil`; **rollback on an error and on a panic** (the panic is re-raised).
- Use `tx`, not `conn`, inside the function; `tx` is bound to the transaction.
- Nested `Transaction` calls use savepoints (`savepoint sp_1` ... `rollback to savepoint` /
  `release savepoint`), so an inner failure can be undone without aborting the outer
  transaction.
- A `db.New(sqlTx, dialect)` built on an existing `*sql.Tx` also uses savepoints.
- `conn.TransactionOpts(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable, ReadOnly: true}, fn)`
  sets isolation and read-only. Options are ignored for nested savepoints.
- A connection that cannot begin transactions returns `db.ErrNoTransaction`.

Row locks (`LockForUpdate`, `SharedLock`) belong inside a transaction.

## Hooks and logging

```go
conn := db.New(sqlDB, db.Postgres, db.WithHook(func(ctx context.Context, e db.QueryEvent) {
    slog.InfoContext(ctx, "sql", "query", e.SQL, "args", e.NumArgs, "took", e.Duration, "err", e.Err)
}))
```

| `QueryEvent` field | |
| --- | --- |
| `SQL` | the statement with placeholders |
| `NumArgs` | how many arguments were bound |
| `Args` | the bound values; only set with `db.WithLogArgs()` |
| `Duration` | time spent in the driver call |
| `Err` | the driver error, if any |

Bind values are left out by default because they routinely hold personal data. Turn
`WithLogArgs` on only where that data may be logged.

## Raw statements

For SQL the builder cannot express, with your dialect's placeholders (`$1` or `?`):

```go
var rows []map[string]any
err := conn.Query(ctx, &rows, "select * from users where age > $1", 30)

n, err := conn.Exec(ctx, "update users set active = $1 where last_seen < $2", false, cutoff)
```

`Query` scans like `Get`; `Exec` returns the rows affected. Hooks fire for both.
