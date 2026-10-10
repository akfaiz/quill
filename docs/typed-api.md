# Typed API

The core API fills a destination you pass in (`Get(ctx, &users)`) and takes maps for writes. The
typed API returns typed values and takes structs, and can find the table from the struct itself.

- [Naming the table](#naming-the-table) · [From and As](#from-and-as) · [Reading](#reading)
- [The typed chain](#the-typed-chain) · [Writing structs](#writing-structs) · [Returning typed rows](#returning-typed-rows)
- [Scalar helpers](#scalar-helpers) · [Row and Rows](#row-and-rows)

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
| `db.As[T](builder)` | the builder you pass | maps, scalars, ad-hoc or dynamic tables, a builder you already built |

```go
users, err := db.From[User](conn).Where("active", true).Get(ctx)          // []User

ids, err   := db.As[int64](conn.Table("users").Select("id")).Get(ctx)      // []int64
rows, err  := db.As[map[string]any](conn.Table("users")).Get(ctx)          // []map[string]any
tenantUsers, err := db.As[User](conn.Table("tenant_" + tenantID + "_users")).Get(ctx) // dynamic table
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
| `Count`, `Exists`, `DoesntExist`, `Sum`, `Avg` | as on the builder |

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

Go methods cannot take type parameters, so the builder's `Get(ctx, &dest)` cannot return `[]T`.
That is why `Query[T]` is a separate type: it mirrors the most used builder methods and returns
itself, so `T` survives the chain.

- **Select and where:** `Select`, `AddSelect`, `SelectRaw`, `Distinct`, `Where`, `OrWhere`,
  `WhereNot`, `WhereRaw`, `OrWhereRaw`, `WhereColumn`, `WhereIn`, `OrWhereIn`, `WhereNotIn`,
  `WhereNull`, `OrWhereNull`, `WhereNotNull`, `WhereBetween`, `WhereNotBetween`, `WhereLike`,
  `WhereGroup`, `OrWhereGroup`, `WhereExists`, `WhereNotExists`
- **Joins:** `Join`, `LeftJoin`, `RightJoin`, `JoinFunc`, `LeftJoinFunc`
- **Grouping, order, paging:** `GroupBy`, `Having`, `HavingRaw`, `OrderBy`, `OrderByDesc`,
  `OrderByRaw`, `Latest`, `Oldest`, `Limit`, `Offset`, `ForPage`
- **Other:** `LockForUpdate`, `SharedLock`, `AllRows`, `Returning`
- **Control:** `When`, `Unless`, `Clone`, `Builder`

Each forwards to the builder method of the same name; a test keeps them identical. For anything
else, `Apply` hands you the builder:

```go
db.From[User](conn).
    Apply(func(b *db.Builder) { b.WhereDate("created_at", day).WhereJsonContains("meta->tags", "go") }).
    OrderBy("id").
    Get(ctx)
```

`Builder()` returns the underlying builder (the same one, not a copy).

## Writing structs

Writes take structs and use the same `db` tags as scanning:

```go
id, err := db.From[User](conn).InsertGetID(ctx, User{Name: "Ann"})
n, err  := db.From[User](conn).Insert(ctx, u1, u2)
n, err   = db.From[User](conn).InsertOrIgnore(ctx, u1)
n, err   = db.From[User](conn).Upsert(ctx, []User{u1}, []string{"email"}, []string{"name"})
n, err   = db.From[User](conn).Where("id", id).Update(ctx, User{Name: "Anna"})
n, err   = db.From[User](conn).Where("id", id).UpdateMap(ctx, map[string]any{"email": nil}) // explicit NULL
n, err   = db.From[User](conn).Where("id", id).Delete(ctx)
err      = db.From[User](conn).Truncate(ctx)
```

### Tags

| Tag | Effect |
| --- | --- |
| `db:"name"` | the column name (default: snake_case of the field name) |
| `db:"name,omitempty"` | left out of writes while the field holds its zero value |
| `db:",omitempty"` | default name, `omitempty` |
| `db:"-"` | never read or written |

### Rules

- **Zero values with `omitempty` are skipped**, so the database default or the existing value
  stays. To write a zero value, use a pointer field (`Age *int` with `&zero`) or `UpdateMap`.
- **Pointers and `sql.Null*`:** a nil pointer is written as `NULL`; `sql.Null*` types and any
  `driver.Valuer` work as usual; a `db.Raw(...)` field value is inlined.
- **Batches:** every row of a batch insert must have the same columns, so an `omitempty` column
  is left out only when it is zero in **every** row. If any row sets it, all rows write it.
- **Embedded structs** are flattened. A field behind a nil embedded pointer counts as omitted
  (or `NULL` in a batch where another row sets it).
- Unexported fields are ignored.
- `Update` refuses to run without a `WHERE` (`db.ErrNoWhere`) and returns `db.ErrNoValues` when
  every field was omitted. `Delete` is guarded the same way. See
  [the guard](writing-data.md#the-where-guard).
- A pointer to a struct also works: `db.As[*User](b).Insert(ctx, &u)`.

## Returning typed rows

On PostgreSQL and SQLite:

```go
rows, err := db.From[User](conn).InsertReturning(ctx, User{Name: "Rae"})            // []User, all columns
rows, err  = db.From[User](conn).Where("id", 7).UpdateReturning(ctx, User{Name: "Rae2"})
rows, err  = db.From[User](conn).Where("id", 7).DeleteReturning(ctx)
```

The `RETURNING` columns default to `*`; narrow them with `.Returning("id", "name")`. MySQL returns
`db.ErrUnsupported`.

## Scalar helpers

For a single value or column, without a struct:

```go
name, err   := db.ValueAs[string](ctx, conn.Table("users").Where("id", 7), "name")
ids, err    := db.PluckAs[int64](ctx, conn.Table("users").OrderBy("id"), "id")
oldest, err := db.MinAs[time.Time](ctx, conn.Table("users"), "created_at")
newest, err := db.MaxAs[time.Time](ctx, conn.Table("users"), "created_at")
total, err  := db.AggregateAs[float64](ctx, conn.Table("orders"), "sum", "total")
```

`NULL` comes back as the zero value of the type. `db.As[int64](...)` with a single-column query
gives the same results through `Query[T]`.

## Row and Rows

`db.Row(v)` and `db.Rows(vs)` turn structs into the `map[string]any` form the write methods and the
`...SQL` builders take, applying the tag rules above:

```go
row, err := db.Row(User{Name: "Ann"})                       // map[string]any{"name": "Ann", "email": (*string)(nil)}
sql, args, err := db.Use(db.Postgres).Table("users").InsertSQL(row)

rows, err := db.Rows([]User{u1, u2})                        // one map per struct, same columns
```

`Row` accepts a struct or non-nil pointer; anything else (a map, a nil, a scalar) is
`db.ErrInvalidArgs`.
