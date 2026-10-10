# Building queries

Every example uses `conn := db.New(...)` or `db.Use(...)` and shows the PostgreSQL SQL unless
noted. `ToSQL()` returns `(sql string, args []any, err error)`.

- [Select](#select) · [From](#from) · [Where](#where) · [Dates](#date-and-time-parts)
- [JSON](#json) · [Joins](#joins) · [Grouping](#grouping-and-having) · [Ordering](#ordering)
- [Limit and offset](#limit-and-offset) · [Unions](#unions) · [Subqueries](#subqueries)
- [Locks and index hints](#locks-and-index-hints) · [Raw SQL](#raw-sql) · [Conditional building](#conditional-building)
- [Inspecting SQL](#inspecting-sql)

## Select

```go
q := conn.Table("users").Select("id", "name as n", "u.email")   // SELECT "id", "name" AS "n", "u"."email" ...
q.AddSelect("age")                                              // append
q.Distinct()                                                    // SELECT DISTINCT
q.SelectRaw("count(*) filter (where active) as active_count")   // raw expression
q.SelectSub(conn.Table("posts").SelectRaw("count(*)").WhereColumn("posts.user_id", "users.id"), "post_count")
```

With no columns, `*` is selected. `Select` replaces the list; `AddSelect`, `SelectRaw` and
`SelectSub` append.

## From

```go
conn.Table("users")                    // FROM "users"
conn.Table("users as u")               // FROM "users" AS "u"
conn.Table("app.users")                // FROM "app"."users"
conn.NewQuery().FromSub(sub, "t")      // FROM (SELECT ...) AS "t"
conn.NewQuery().FromRaw("generate_series(?, ?) g", 1, 3)
```

`NewQuery()` gives a builder with no table yet, for `FromSub` and `FromRaw`.
`Prefix("p_")` on a builder applies a table prefix (or set it once with `db.WithPrefix`).

## Where

### Basic forms

```go
Where("votes", 100)              // "votes" = $1
Where("votes", ">", 100)         // "votes" > $1
Where("deleted_at", nil)         // "deleted_at" IS NULL
Where("role", "!=", nil)         // "role" IS NOT NULL
OrWhere("name", "john")          // ... or "name" = $n
WhereNot("active", true)         // ... and not "active" = $n
WhereRaw("lower(name) = ?", "ann")
WhereColumn("updated_at", ">", "created_at")   // "updated_at" > "created_at"
```

Consecutive calls are joined with `and`; `Or...` variants use `or`. The operator must be in the
allow-list (below); a `nil` value is only valid with `=`, `!=`, `<>`, `is` and `is not`.

Allowed operators: `=` `<` `>` `<=` `>=` `<>` `!=` `<=>` `like` `not like` `ilike` `not ilike`
`like binary` `&` `|` `^` `<<` `>>` `&~` `is` `is not` `rlike` `not rlike` `regexp` `not regexp`
`~` `~*` `!~` `!~*` `similar to` `not similar to` `~~*` `!~~*`. Anything else is
`db.ErrInvalidOperator`.

### Grouping

```go
conn.Table("users").
    Where("active", true).
    WhereGroup(func(q *db.Builder) {
        q.Where("role", "admin").OrWhereNull("deleted_at")
    })
// WHERE "active" = $1 AND ("role" = $2 OR "deleted_at" IS NULL)

WhereMap(map[string]any{"b": 2, "a": 1})   // WHERE ("a" = $1 AND "b" = $2)  (keys sorted)
```

`OrWhereGroup` joins with `or`. An empty group is skipped.

### In, between, null, like

```go
WhereIn("id", []int{1, 2, 3})            // "id" in ($1, $2, $3)
WhereIn("id", conn.Table("bans").Select("user_id"))   // "id" IN (SELECT "user_id" FROM "bans")
WhereNotIn("status", []string{"a", "b"})
WhereIntegerInRaw("id", []int{1, 2})     // "id" IN (1, 2)   integers inlined, no bind args
WhereNull("deleted_at")                  // WhereNotNull, OrWhereNull, OrWhereNotNull
WhereBetween("age", 18, 65)              // "age" BETWEEN $1 AND $2   (WhereNotBetween)
WhereBetweenColumns("age", "min_age", "max_age")
WhereValueBetween(10, "low", "high")     // $1 BETWEEN "low" AND "high"
WhereLike("name", "a%")                  // case-insensitive
WhereLike("name", "A%", true)            // case-sensitive
WhereNotLike("name", "%x")
WhereNullSafeEquals("a", nil)            // NULL-safe equality
```

An empty slice for `WhereIn` compiles to `0 = 1` (matches nothing) and for `WhereNotIn` to
`1 = 1` (matches everything), so an empty filter list never produces invalid SQL.
`WhereIntegerInRaw` is for very large id lists that would exceed a driver's parameter limit; it
rejects anything that is not an integer.

`WhereLike` renders per dialect: `ILIKE`/`LIKE` (PostgreSQL), `LIKE`/`LIKE BINARY` (MySQL),
`LIKE`/`GLOB` (SQLite, with the pattern converted). See [Dialects](dialects.md).

### Several columns, rows, existence

```go
WhereAll([]string{"first", "last"}, "like", "a%")   // ("first" LIKE $1 AND "last" LIKE $2)
WhereAny([]string{"name", "email"}, "like", "b%")   // ("name" LIKE $1 OR "email" LIKE $2)
WhereNone([]string{"name", "email"}, "like", "z%")  // NOT ("name" LIKE $1 OR "email" LIKE $2)
WhereRowValues([]string{"id", "age"}, "=", []any{1, 30})   // ("id", "age") = ($1, $2)

posts := conn.NewQuery().From("posts").WhereColumn("posts.user_id", "users.id")
conn.Table("users").WhereExists(posts)       // EXISTS (SELECT * FROM "posts" WHERE ...)
conn.Table("users").WhereNotExists(posts)
```

## Date and time parts

```go
WhereDate("created_at", "2024-03-07")        // or a time.Time
WhereTime("created_at", ">=", "09:00:00")
WhereDay("created_at", 7)                    // numeric day and month are zero-padded
WhereMonth("created_at", 3)
WhereYear("created_at", ">", 2023)
```

A `time.Time` is formatted for the part (`2006-01-02`, `15:04:05`, `02`, `01`, `2006`).
PostgreSQL uses `::DATE`, `::TIME` and `EXTRACT(...)`; MySQL uses `DATE()`, `DAY()` and friends;
SQLite uses `STRFTIME(...)`.

## JSON

Reference a JSON path with `->`, in selects, wheres, ordering and updates:

```go
Select("options->theme")                       // "options"->>'theme'       (PostgreSQL)
Where("options->theme", "dark")
Where("options->enabled", true)                // compares as a JSON boolean
OrderBy("options->rank")
Select("meta->tags[0]")                        // array index
WhereJsonContains("options->tags", "go")
WhereJsonDoesntContain("options->tags", "go")
WhereJsonContainsKey("options->theme")
WhereJsonLength("options->tags", ">", 2)
WhereJsonOverlaps("options->tags", []string{"a", "b"})   // MySQL and MariaDB
```

Every `WhereJson*` method has an `Or` variant. Updating a path is covered in
[Writing data](writing-data.md#updating-json-paths). Per-dialect SQL differs a lot; see
[Dialects](dialects.md#json). Single quotes in a path are escaped, so a path cannot break out
of its SQL literal.

## Joins

```go
conn.Table("users as u").
    Join("posts as p", "p.user_id", "u.id").          // INNER JOIN ... ON "p"."user_id" = "u"."id"
    LeftJoin("profiles", "profiles.user_id", "=", "u.id").
    RightJoin("teams", "teams.id", "<>", "u.team_id").
    CrossJoin("sizes")
```

Column-to-column joins take `(second)` or `(operator, second)`.

### Complex conditions

```go
conn.Table("users").JoinFunc("contacts", func(j *db.JoinClause) {
    j.On("users.id", "contacts.user_id").
        OrOn("users.alt_id", "contacts.user_id").
        Where("contacts.kind", "primary").          // a bound value
        WhereNull("contacts.deleted_at").
        OnGroup(func(g *db.JoinClause) {
            g.Where("contacts.score", ">", 5).OrWhereIn("contacts.tag", []string{"x", "y"})
        })
})
```

`JoinClause` offers `On`, `OrOn`, `OnGroup`, `OrOnGroup`, `Where`, `OrWhere`, `WhereIn`,
`OrWhereIn`, `WhereNotIn`, `OrWhereNotIn`, `WhereNull`, `OrWhereNull`, `WhereNotNull`,
`OrWhereNotNull`, `WhereRaw` and `OrWhereRaw`. A join other than `CrossJoin` with no condition
fails with `db.ErrJoinNeedsOn`.

Closure forms: `JoinFunc`, `LeftJoinFunc`, `RightJoinFunc`. Join on a bound value:
`JoinWhere`, `LeftJoinWhere`, `RightJoinWhere`.

### Subquery and lateral joins

```go
latest := conn.NewQuery().From("posts").Select("user_id").SelectRaw("max(created_at) as last").GroupBy("user_id")

conn.Table("users").JoinSub(latest, "l", "l.user_id", "users.id")   // also LeftJoinSub, RightJoinSub
conn.Table("users").CrossJoinSub(latest, "l")
conn.Table("users").JoinSubFunc(latest, "l", func(j *db.JoinClause) { j.On("l.user_id", "users.id") })

top := conn.NewQuery().From("posts").WhereColumn("posts.user_id", "users.id").OrderByDesc("views").Limit(1)
conn.Table("users").JoinLateral(top, "p")        // INNER JOIN LATERAL (...) AS "p" ON TRUE
conn.Table("users").LeftJoinLateral(top, "p")    // PostgreSQL and MySQL 8.0.14+; not MariaDB or SQLite
```

`StraightJoin` works on MySQL and MariaDB. Lateral joins work on PostgreSQL and MySQL only (not MariaDB or
SQLite). Unsupported joins return `db.ErrUnsupported`.

## Grouping and having

```go
conn.Table("orders").
    Select("user_id").SelectRaw("sum(total) as s").
    GroupBy("user_id").
    HavingRaw("sum(total) > ?", 100)

Having("count", ">", 1)      // OrHaving
HavingNull("x")              // HavingNotNull
HavingBetween("n", 1, 5)     // HavingNotBetween
HavingGroup(func(q *db.Builder) { q.Having("a", 1).OrHavingRaw("b > ?", 2) })
GroupByRaw("date(created_at)")
```

Use `HavingRaw` (not an alias) for aggregates: PostgreSQL does not allow output aliases in
`HAVING`.

## Ordering

```go
OrderBy("name")                  // asc
OrderBy("name", "desc")          // direction must be "asc" or "desc"
OrderByDesc("created_at")
OrderByRaw("random()")
Latest()                         // ORDER BY "created_at" DESC   (Latest("col"))
Oldest("updated_at")
InRandomOrder()                  // RANDOM() / RAND(); InRandomOrder(42) seeds MySQL
Reorder()                        // drop all ordering
ReorderBy("id", "desc")          // drop, then order
```

`ForPageAfterID(perPage, lastID, column...)` and `ForPageBeforeID(...)` build keyset paging:
`WHERE "id" > $1 ORDER BY "id" ASC LIMIT n` (a `nil` last id selects `IS NOT NULL`). Existing
conditions that contain an `OR` or a raw fragment are wrapped in parentheses first, so
`Where("a", 1).OrWhere("b", 2)` becomes `("a" = $1 OR "b" = $2) AND "id" > $3`. `Find`,
`CursorPaginate`, `ChunkByID` and `UpdateOrInsert` group their added conditions the same way.

Keyset paging on a query with `Union` returns `ErrInvalidArgs`: the key condition would apply to the
first member only, so every page would re-read the others. Wrap the union with `FromSub` and page the
wrapper. `ChunkByID` also replaces any `OrderBy` with the key order, since the last row of a page has
to hold the highest key.

## Limit and offset

```go
Limit(10)            // Take(10)
Offset(20)           // Skip(20)
ForPage(3, 15)       // OFFSET 30 LIMIT 15
```

A negative limit is ignored and a negative offset becomes 0. An offset with no limit works on
every dialect (MySQL and SQLite get the unbounded limit they require).

## Unions

```go
a := conn.Table("a").Select("id").Where("x", 1)
b := conn.Table("b").Select("id")

a.Union(b).UnionAll(conn.Table("c").Select("id")).OrderBy("id").Limit(5)
// (SELECT "id" FROM "a" WHERE "x" = $1) UNION (SELECT "id" FROM "b") UNION ALL (SELECT "id" FROM "c") ORDER BY "id" ASC LIMIT 5
```

After the first union, `OrderBy`, `Limit` and `Offset` apply to the combined result. Ordering and
limits set before the union stay on the first query. SQLite wraps each member as
`SELECT * FROM (...)`.

## Subqueries

Pass a `*db.Builder` where a value, list or table is expected:

```go
Where("score", ">", conn.Table("scores").SelectRaw("avg(v)"))   // scalar subquery
WhereIn("id", conn.Table("bans").Select("user_id"))
WhereExists(sub); WhereNotExists(sub)
SelectSub(sub, "alias"); FromSub(sub, "alias"); JoinSub(sub, "alias", ...)
```

Bind arguments are numbered across the whole statement, so `$1, $2, ...` stay correct however
deeply queries nest. Build the inner query with `conn.NewQuery().From("t")` or
`conn.Table("t")`. Subqueries are copied when added.

## Common table expressions

`With` adds a named query in front of the statement. Reference it by name in `From`, joins or
subqueries:

```go
active := conn.Table("users").Where("active", true)

conn.Table("au").With("au", active).Where("age", ">", 30)
// WITH "au" AS (SELECT * FROM "users" WHERE "active" = $1) SELECT * FROM "au" WHERE "age" > $2

conn.Table("a").With("a", conn.Table("users").Select("id", "name"), "uid", "uname")
// WITH "a" ("uid", "uname") AS (SELECT "id", "name" FROM "users") SELECT * FROM "a"
```

`WithRecursive` takes the anchor query with the recursive member added by `UnionAll` (or `Union`).
The clause becomes `WITH RECURSIVE`, and the members are written without parentheses, because
SQLite rejects a recursive reference inside a subquery:

```go
anchor := conn.Table("categories").Where("id", 1)
step := conn.Table("categories as c").Select("c.*").Join("tree", "tree.id", "c.parent_id")

conn.Table("tree").WithRecursive("tree", anchor.UnionAll(step))
// WITH RECURSIVE "tree" AS (SELECT * FROM "categories" WHERE "id" = $1 UNION ALL
//   SELECT "c".* FROM "categories" AS "c" INNER JOIN "tree" ON "tree"."id" = "c"."parent_id")
//   SELECT * FROM "tree"
```

- `WithRaw(name, db.Raw(sql, args...), cols...)` uses caller-trusted SQL as the body.
- `WithMaterialized` and `WithNotMaterialized` add `as [not] materialized` (PostgreSQL only; other
  dialects return `ErrUnsupported`).
- A CTE name is a table reference, so the table prefix applies to it just as it does in `From`.
- The query passed in is copied when added. Duplicate or qualified names return `ErrInvalidArgs`.
- WITH belongs to a select. A select carrying CTEs works as a subquery, a union member, in
  `Count`/`Exists`/`Paginate`, and as the source of `InsertUsing`
  (`INSERT INTO t (cols) WITH ... SELECT ...`). Calling `With` on the builder that runs
  `Insert`, `Update`, `Delete`, `Upsert` or `Truncate` returns `ErrUnsupported`; filter the write
  with `WhereIn("id", cteQuery)` instead.

## Locks and index hints

```go
LockForUpdate()          // FOR UPDATE               (PostgreSQL, MySQL)
SharedLock()             // FOR SHARE / LOCK IN SHARE MODE
Lock("for no key update")   // caller-trusted raw clause
UseIndex("idx_a, idx_b"); ForceIndex("idx_a"); IgnoreIndex("idx_a")   // MySQL; ForceIndex on SQLite
```

SQLite ignores row locks. PostgreSQL ignores index hints. Index names are emitted unquoted and
validated against `[a-zA-Z0-9_$]`; anything else is `db.ErrInvalidArgs`. Run locks inside a
transaction.

## Raw SQL

```go
db.Raw("now() - interval ?", "1 day")      // an Expression: SQL text plus bind arguments
Where("created_at", ">", db.Raw("now() - interval ?", "1 day"))
SelectRaw("count(*) filter (where x > ?) as c", 5)
```

In raw text, `?` is a bind marker (translated to `$n` on PostgreSQL) and `??` is a literal `?`.
Markers inside quoted strings or identifiers are left alone.

> Raw SQL text is inserted as written. Never build it from user input. Pass user values as
> arguments (`?`), never by string concatenation. See [Security](security.md).

## Conditional building

```go
q := conn.Table("users").
    When(onlyActive, func(q *db.Builder) { q.Where("active", true) }).
    Unless(includeDeleted, func(q *db.Builder) { q.WhereNull("deleted_at") }).
    Tap(func(q *db.Builder) { log.Println("building users query") })
```

## Inspecting SQL

```go
q := conn.Table("users").Where("id", 7)

sql, args, err := q.ToSQL()     // statement with placeholders, plus the ordered arguments
raw, err      := q.ToRawSQL()   // arguments interpolated: DEBUG ONLY, never execute or log it
sql, args      = q.MustSQL()    // panics instead of returning the error
sql, args, err  = q.ExistsSQL() // SELECT EXISTS(...) AS "exists"
sql, args, err  = q.AggregateSQL("sum", "total")
```

The write statements have matching `...SQL` forms; see [Writing data](writing-data.md).
