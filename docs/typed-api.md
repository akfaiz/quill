# Typed API

The core API fills a destination you pass in (`Get(ctx, &users)`). The typed API (`db.Query[T]`)
returns typed values and can find the table from the struct itself. It is **select-only**: writes
with structs go through the write builders (`Insert(...).Rows(...)`, `Update(...).SetRow(...)`).

- [Naming the table](#naming-the-table) · [From and As](#from-and-as) · [Reading](#reading)
- [The typed chain](#the-typed-chain) · [Struct writes](#struct-writes) · [Returning rows](#returning-rows)
- [Scalar helpers](#scalar-helpers) · [Row](#row)

## Naming the table

Give the row type a `TableName()` method:

```go
type User struct {
    ID    int64   `db:"id,omitempty"`
    Name  string  `db:"name"`
    Email *string `db:"email"`
}

func (User) TableName() string { return "users" }
```

- A value or pointer receiver both work, and the method is promoted through embedding (an outer
  type's own `TableName` wins).
- It is called on a **zero value**, **once per type**, and the result is cached. A name that
  changes later is not picked up.
- A method that panics on a zero value (for example by reading a nil embedded pointer) is
  reported as `db.ErrInvalidArgs` naming the type, instead of crashing your program.
- A type with no `TableName()`, or one returning `""`, is `db.ErrInvalidArgs`. The table is never
  guessed.
- `db.TableName[User]()` returns the name directly.
- For per-tenant or per-period tables, give the name yourself with [`As`](#from-and-as).

## From and As

| Entry point | Table comes from | Use it for |
| --- | --- | --- |
| `db.From[T](conn)` | `T.TableName()` | the common case |
| `db.As[T](sel)` | the `*db.SelectBuilder` you pass | maps, scalars, ad-hoc or dynamic tables, a builder you already built |

```go
users, err := db.From[User](conn).Where("active", true).Get(ctx)          // []User

ids, err   := db.As[int64](conn.Select("id").From("users")).Get(ctx)         // []int64
rows, err  := db.As[map[string]any](conn.Select().From("users")).Get(ctx)     // []map[string]any
tenantUsers, err := db.As[User](conn.Select().From("tenant_" + tenantID + "_users")).Get(ctx) // dynamic table
```

A problem with `T` (no `TableName`) does not break the chain; it is reported by the first
terminal call, like any builder error. Both work with `db.Use(...)` to build SQL only:
`db.From[User](db.Use(db.Postgres)).Where("id", 7).Builder().ToSQL()`.

## Reading

`Query[T]` returns `T` instead of filling a pointer.

| Method | Returns |
| --- | --- |
| `Get(ctx)` | `[]T` |
| `First(ctx)` | `T`, or `db.ErrNotFound` |
| `Find(ctx, id)` | `T` where `id = ?` |
| `Sole(ctx)` | the only `T`; `ErrNotFound` or `ErrMultipleRecords` |
| `Paginate(ctx, page, perPage)` | `db.Paged[T]` |
| `SimplePaginate(ctx, page, perPage)` | `db.SimplePaged[T]` |
| `CursorPaginate(ctx, perPage, cursor, opts...)` | `db.CursorPaged[T]` |
| `Chunk(ctx, size, fn func([]T) (bool, error))` | |
| `ChunkByID(ctx, size, fn, column...)` | |
| `Each(ctx, fn func(T) error)` | |
| `Stream(ctx)` | `iter.Seq2[T, error]` |
| `Count`, `Exists`, `DoesntExist`, `Sum`, `Avg` | as on the select builder |

The paged types embed the page description and add the rows:

```go
page, err := db.From[User](conn).OrderBy("id").Paginate(ctx, 2, 15)
fmt.Println(page.Total, page.LastPage, page.From, page.To) // promoted from db.Page
for _, u := range page.Items { /* use u */ }                // Items is []User
```

`db.Paged[T]` embeds `db.Page`, `db.SimplePaged[T]` embeds `db.SimplePage` (`HasMore`), and
`db.CursorPaged[T]` embeds `db.CursorPage` (`HasMore`, `NextCursor`). Scanning rules are the ones in
[Running queries](running-queries.md#how-rows-are-scanned).

## The typed chain

Go methods cannot take type parameters, so the select builder's `Get(ctx, &dest)` cannot return
`[]T`. That is why `Query[T]` is a separate type: it mirrors the select builder's chain methods
(sources, `Select*`, every `Where*`, joins, grouping and `Having*`, ordering, limit and offset,
unions, CTEs, locks and index hints) and returns itself, so `T` survives the chain. The mirror is
generated from the select builder, so the two always match.

Two methods take a `Query[T]` or a clause instead of a builder: `When` and `Unless` receive
`func(*db.Query[T])`, and `WhereGroup` / `HavingGroup` receive `*db.WhereClause` / `*db.HavingClause`.
For anything not mirrored, `Apply` hands you the underlying select builder:

```go
db.From[User](conn).
    Apply(func(b *db.SelectBuilder) { b.WhereDate("created_at", day).WhereJsonContains("meta->tags", "go") }).
    OrderBy("id").
    Get(ctx)
```

`Builder()` returns the underlying `*db.SelectBuilder` (the same one, not a copy). The full list
of `Query[T]` methods is in the [reference](reference.md#method-index).

## Struct writes

Writes are not part of `Query[T]`. Use the write builders with the same `db` tags as scanning:

```go
id, err := conn.Insert("users").Rows(User{Name: "Ann"}).ExecGetID(ctx)
n, err  := conn.Insert("users").Rows(u1, u2).Exec(ctx)                // or Rows([]User{u1, u2})
n, err   = conn.Insert("users").Rows(u1).Ignore().Exec(ctx)
n, err   = conn.Insert("users").Rows(u1).Upsert([]string{"email"}, []string{"name"}).Exec(ctx)
n, err   = conn.Update("users").SetRow(User{Name: "Anna"}).Where("id", id).Exec(ctx)
n, err   = conn.Update("users").SetMap(map[string]any{"email": nil}).Where("id", id).Exec(ctx) // explicit NULL
n, err   = conn.Delete("users").Where("id", id).Exec(ctx)
err      = conn.Truncate(ctx, "users")
```

Take the table name from the type when you do not want to repeat it:
`name, err := db.TableName[User]()`.

### Tags

| Tag | Effect |
| --- | --- |
| `db:"name"` | the column name (default: snake_case of the field name) |
| `db:"name,omitempty"` | left out of writes while the field holds its zero value |
| `db:",omitempty"` | default name, `omitempty` |
| `db:"-"` | never read or written |

### Rules

- **Zero values with `omitempty` are skipped**, so the database default or the existing value
  stays. To write a zero value, use a pointer field (`Age *int` with `&zero`) or `SetMap` / `Set`.
- **Pointers and `sql.Null*`:** a nil pointer is written as `NULL`; `sql.Null*` types and any
  `driver.Valuer` work as usual; a `db.Raw(...)` field value is inlined.
- **Batches:** every row of a batch insert must have the same columns, so an `omitempty` column
  is left out only when it is zero in **every** row of one `Rows` call. If any row sets it, all
  rows write it.
- **Embedded structs** are flattened. A field behind a nil embedded pointer counts as omitted
  (or `NULL` in a batch where another row sets it).
- Unexported fields are ignored.
- `Update(...).SetRow(...)` refuses to run without a `WHERE` (`db.ErrNoWhere`) and returns
  `db.ErrNoValues` when every field was omitted. `Delete` is guarded the same way. See
  [the guard](writing-data.md#the-where-guard).
- A pointer to a struct also works: `Rows(&u)`, `SetRow(&u)`.

## Returning rows

On PostgreSQL and SQLite, scan `RETURNING` rows into a slice of structs with the write builder's
`Get`:

```go
var rows []User
err := conn.Insert("users").Rows(User{Name: "Rae"}).Returning("*").Get(ctx, &rows)
err  = conn.Update("users").SetRow(User{Name: "Rae2"}).Where("id", 7).Returning("*").Get(ctx, &rows)
err  = conn.Delete("users").Where("id", 7).Returning("*").Get(ctx, &rows)
```

`Returning` is required before `Get`. MySQL and MariaDB return `db.ErrUnsupported`.

## Scalar helpers

For a single value or column, without a struct:

```go
name, err   := db.ValueAs[string](ctx, conn.Select().From("users").Where("id", 7), "name")
ids, err    := db.PluckAs[int64](ctx, conn.Select().From("users").OrderBy("id"), "id")
```

`NULL` comes back as the zero value of the type. `db.As[int64](...)` with a single-column query
gives the same results through `Query[T]`.

## Row

`db.Row(v)` turns a struct into the `map[string]any` form that `Values`, `SetMap` and
`UpdateOrInsert` take, applying the tag rules above (`Rows(...)` and `SetRow(...)` on the write
builders do this for you):

```go
row, err := db.Row(User{Name: "Ann"})                       // map[string]any{"name": "Ann", "email": (*string)(nil)}
sql, args, err := db.Use(db.Postgres).Insert("users").Values(row).ToSQL()
```

`Row` accepts a struct or non-nil pointer; anything else (a map, a nil, a scalar) is
`db.ErrInvalidArgs`.
