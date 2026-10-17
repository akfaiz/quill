# Reference

- [Errors](#errors) · [Types](#types) · [Options](#options) · [Package functions](#package-functions)
- [Method index](#method-index)

For signatures and doc comments use `go doc github.com/akfaiz/quill/db` (add a name, for example
`go doc github.com/akfaiz/quill/db SelectBuilder.Where`).

## Errors

All are sentinel values; test them with `errors.Is`. Errors from deeper layers wrap these, so
`errors.Is` keeps working through wrapped chains.

| Error | Returned when |
| --- | --- |
| `db.ErrNotFound` | `First`, `Find`, `Sole`, `Value` matched no row. It is `sql.ErrNoRows` |
| `db.ErrMultipleRecords` | `Sole` matched more than one row |
| `db.ErrNoOrder` | `Chunk` was called without an `ORDER BY` |
| `db.ErrNotBound` | A query ran on a `DB` with no connection (`db.Use`) |
| `db.ErrNoTransaction` | `Transaction` on a connection that cannot begin one |
| `db.ErrUnknownDialect` | The dialect is not `Postgres`, `MySQL` or `SQLite` |
| `db.ErrInvalidOperator` | An operator outside the allow-list |
| `db.ErrInvalidDirection` | An order direction other than `asc` or `desc` |
| `db.ErrNoTable` | The query has no table |
| `db.ErrInvalidArgs` | Arguments the library cannot use: wrong argument count, a non-struct passed to a struct write, an unsafe index name, a missing `TableName()`, a malformed cursor, ... |
| `db.ErrUnsupported` | The feature does not exist on this dialect (`RETURNING` on MySQL, lateral joins on SQLite, ...) |
| `db.ErrJoinNeedsOn` | A join (other than a cross join) has no `ON` condition |
| `db.ErrNoValues` | A write had nothing to write (an insert with no rows, an update with nothing to set, all fields omitted) |
| `db.ErrNoWhere` | An update or delete builder (including `Increment*`) without a `WHERE` or `LIMIT` and without `AllRows()` |

Builder errors are collected while you chain and returned by the terminal call; `Err()` (on every
builder) shows the first one early.

## Types

| Type | Purpose |
| --- | --- |
| `db.Dialect` | `Postgres`, `MySQL`, `MariaDB`, `SQLite` |
| `db.DB` | A dialect plus an optional connection. `db.New` or `db.Use` creates it. Starts statements with `Select`, `Insert`, `Update` and `Delete` |
| `db.Conn` | `QueryContext` and `ExecContext`: satisfied by `*sql.DB`, `*sql.Tx`, `*sql.Conn` |
| `db.SelectBuilder` | A SELECT statement (`DB.Select(...).From(...)`): where, joins, grouping, unions, CTEs, locks, readers |
| `db.InsertBuilder` | An INSERT statement (`DB.Insert(table)`): `Values`, `Rows`, `Ignore`, `Using`, `Upsert`, `Returning` |
| `db.UpdateBuilder` | An UPDATE statement (`DB.Update(table)`): `Set*`, `Increment*`, `FromJoins`, where, joins, order, limit |
| `db.DeleteBuilder` | A DELETE statement (`DB.Delete(table)`): where, joins, order, limit |
| `db.WhereClause` | The conditions of a `WhereGroup` callback |
| `db.HavingClause` | The conditions of a `HavingGroup` callback |
| `db.JoinClause` | Conditions of one join, inside `JoinFunc` and friends |
| `db.Expression` | Raw SQL plus arguments; create it with `db.Raw` |
| `db.Query[T]` | The typed, select-only query (`db.From[T]`, `db.As[T]`) |
| `db.Tabler` | `TableName() string`, implemented by row types |
| `db.Option` | A `db.New` / `db.Use` option |
| `db.QueryEvent` | `SQL`, `NumArgs`, `Args`, `Duration`, `Err`: passed to hooks |
| `db.Stmt` | `SQL` and `Args` of one statement (`DB.TruncateSQL`) |
| `db.Page` | `Total int64`, `PerPage`, `CurrentPage`, `LastPage`, `From`, `To` |
| `db.SimplePage` | `PerPage`, `CurrentPage`, `HasMore` |
| `db.CursorPage` | `PerPage`, `HasMore`, `NextCursor` |
| `db.CursorOptions` | `Column` (default `"id"`), `Desc` |
| `db.Paged[T]`, `db.SimplePaged[T]`, `db.CursorPaged[T]` | The page types above, with `Items []T` |

## Options

| Option | Effect |
| --- | --- |
| `db.WithPrefix(prefix)` | Table prefix for table names, aliases and qualified columns |
| `db.WithHook(fn)` | `fn(ctx, QueryEvent)` after every statement |
| `db.WithLogArgs()` | Put bind values in `QueryEvent.Args` (off by default) |

CTE options (`db.CTEOption`), passed after the query of `With`, `WithRaw` and `WithRecursive`:

| Option | Effect |
| --- | --- |
| `db.CTEColumns(cols...)` | Name the CTE's columns |
| `db.CTEMaterialized()`, `db.CTENotMaterialized()` | `AS [NOT] MATERIALIZED` (PostgreSQL, SQLite) |
| `db.CTEUnionDistinct()` | `UNION` instead of `UNION ALL` between the anchor and step of `WithRecursive` |

## Package functions

`As`, `CTEColumns`, `CTEMaterialized`, `CTENotMaterialized`, `CTEUnionDistinct`, `From`, `New`, `PluckAs`, `Raw`, `Row`, `Stream`,
`TableName`, `Use`, `ValueAs`, `WithHook`, `WithLogArgs`, `WithPrefix`

There is no builder constructor besides the `DB` methods: start with `db.Use(dialect).Select()` (or
`Insert`, `Update`, `Delete`) when you only need SQL.

## Method index

Every exported method, grouped by type and purpose. The guides explain each group. The select, update
and delete builders and the two clause types share the same `Where*` family; the old single
builder type no longer exists.

### `db.DB` (12)

`Delete`, `Dialect`, `Exec`, `Insert`, `Query`, `Select`, `Transaction`, `TransactionOpts`,
`Truncate`, `TruncateSQL`, `Update`, `UpdateOrInsert`

`Select`, `Insert`, `Update` and `Delete` start a statement; `Truncate`, `TruncateSQL` and `UpdateOrInsert` are statement-level helpers that need no builder.

### `db.SelectBuilder` (181)

Started with `DB.Select(columns...)`. Chain methods return `*db.SelectBuilder`; the build and read methods end the chain.

**Build SQL** (6)

`AggregateSQL`, `ExistsSQL`, `MustRawSQL`, `MustSQL`, `ToRawSQL`, `ToSQL`

**Select and source** (9)

`AddSelect`, `Distinct`, `From`, `FromRaw`, `FromSub`, `Prefix`, `Select`, `SelectRaw`, `SelectSub`

**Where** (64)

`OrWhere`, `OrWhereAll`, `OrWhereAny`, `OrWhereBetween`, `OrWhereBetweenColumns`, `OrWhereColumn`,
`OrWhereDate`, `OrWhereDay`, `OrWhereExists`, `OrWhereGroup`, `OrWhereIn`, `OrWhereIntegerInRaw`,
`OrWhereIntegerNotInRaw`, `OrWhereLike`, `OrWhereMap`, `OrWhereMonth`, `OrWhereNone`, `OrWhereNot`,
`OrWhereNotBetween`, `OrWhereNotBetweenColumns`, `OrWhereNotExists`, `OrWhereNotIn`,
`OrWhereNotLike`, `OrWhereNotNull`, `OrWhereNull`, `OrWhereNullSafeEquals`, `OrWhereRaw`,
`OrWhereRowValues`, `OrWhereTime`, `OrWhereValueBetween`, `OrWhereValueNotBetween`, `OrWhereYear`,
`Where`, `WhereAll`, `WhereAny`, `WhereBetween`, `WhereBetweenColumns`, `WhereColumn`, `WhereDate`,
`WhereDay`, `WhereExists`, `WhereGroup`, `WhereIn`, `WhereIntegerInRaw`, `WhereIntegerNotInRaw`,
`WhereLike`, `WhereMap`, `WhereMonth`, `WhereNone`, `WhereNot`, `WhereNotBetween`,
`WhereNotBetweenColumns`, `WhereNotExists`, `WhereNotIn`, `WhereNotLike`, `WhereNotNull`,
`WhereNull`, `WhereNullSafeEquals`, `WhereRaw`, `WhereRowValues`, `WhereTime`, `WhereValueBetween`,
`WhereValueNotBetween`, `WhereYear`

**JSON conditions** (14)

`OrWhereJsonContains`, `OrWhereJsonContainsKey`, `OrWhereJsonDoesntContain`,
`OrWhereJsonDoesntContainKey`, `OrWhereJsonDoesntOverlap`, `OrWhereJsonLength`,
`OrWhereJsonOverlaps`, `WhereJsonContains`, `WhereJsonContainsKey`, `WhereJsonDoesntContain`,
`WhereJsonDoesntContainKey`, `WhereJsonDoesntOverlap`, `WhereJsonLength`, `WhereJsonOverlaps`

**Group and having** (16)

`GroupBy`, `GroupByRaw`, `Having`, `HavingBetween`, `HavingGroup`, `HavingNotBetween`,
`HavingNotNull`, `HavingNull`, `HavingRaw`, `OrHaving`, `OrHavingBetween`, `OrHavingGroup`,
`OrHavingNotBetween`, `OrHavingNotNull`, `OrHavingNull`, `OrHavingRaw`

**Joins** (20)

`CrossJoin`, `CrossJoinSub`, `Join`, `JoinFunc`, `JoinLateral`, `JoinSub`, `JoinSubFunc`,
`JoinWhere`, `LeftJoin`, `LeftJoinFunc`, `LeftJoinLateral`, `LeftJoinSub`, `LeftJoinSubFunc`,
`LeftJoinWhere`, `RightJoin`, `RightJoinFunc`, `RightJoinSub`, `RightJoinSubFunc`, `RightJoinWhere`,
`StraightJoin`

**Ordering and paging** (15)

`ForPage`, `ForPageAfterID`, `ForPageBeforeID`, `InRandomOrder`, `Latest`, `Limit`, `Offset`,
`Oldest`, `OrderBy`, `OrderByDesc`, `OrderByRaw`, `Reorder`, `ReorderBy`, `Skip`, `Take`

**Common table expressions** (3)

`With`, `WithRaw`, `WithRecursive`

**Unions, locks, index hints** (8)

`ForceIndex`, `IgnoreIndex`, `Lock`, `LockForUpdate`, `SharedLock`, `Union`, `UnionAll`, `UseIndex`

**Control flow** (5)

`Clone`, `Err`, `Tap`, `Unless`, `When`

**Read** (21)

`Aggregate`, `Avg`, `Chunk`, `ChunkByID`, `Count`, `CursorPaginate`, `DoesntExist`, `Each`,
`Exists`, `Find`, `First`, `Get`, `Max`, `Min`, `Paginate`, `Pluck`, `PluckMap`, `SimplePaginate`,
`Sole`, `Sum`, `Value`

### `db.InsertBuilder` (17)

Started with `DB.Insert(table)`. An insert has no `Where`, `Join`, `OrderBy` or `Limit`. Its CTEs need `Using`.

**Build SQL** (2)

`IDSQL`, `ToSQL`

**Select and source** (1)

`Prefix`

**Common table expressions** (3)

`With`, `WithRaw`, `WithRecursive`

**Control flow** (3)

`Clone`, `Err`, `Returning`

**Write** (8)

`Exec`, `ExecGetID`, `Get`, `Ignore`, `Rows`, `Upsert`, `Using`, `Values`

### `db.UpdateBuilder` (127)

Started with `DB.Update(table)`. Without a `WHERE` or `LIMIT`, `Exec`, `Get` and `ToSQL` return `ErrNoWhere` unless `AllRows` was called. No grouping, `Offset`, unions or locks. CTEs are written before `UPDATE` (not on MariaDB).

**Build SQL** (1)

`ToSQL`

**Select and source** (1)

`Prefix`

**Where**: the same `Where*` / `OrWhere*` methods as `SelectBuilder` (64), including `WhereGroup` and `WhereExists`.

**JSON conditions**: the same `WhereJson*` / `OrWhereJson*` methods as `SelectBuilder` (14).

**Joins** (21)

`CrossJoin`, `CrossJoinSub`, `FromJoins`, `Join`, `JoinFunc`, `JoinLateral`, `JoinSub`,
`JoinSubFunc`, `JoinWhere`, `LeftJoin`, `LeftJoinFunc`, `LeftJoinLateral`, `LeftJoinSub`,
`LeftJoinSubFunc`, `LeftJoinWhere`, `RightJoin`, `RightJoinFunc`, `RightJoinSub`,
`RightJoinSubFunc`, `RightJoinWhere`, `StraightJoin`

**Ordering and paging** (10)

`InRandomOrder`, `Latest`, `Limit`, `Oldest`, `OrderBy`, `OrderByDesc`, `OrderByRaw`, `Reorder`,
`ReorderBy`, `Take`

**Common table expressions** (3)

`With`, `WithRaw`, `WithRecursive`

**Control flow** (4)

`AllRows`, `Clone`, `Err`, `Returning`

**Write** (9)

`Decrement`, `DecrementEach`, `Exec`, `Get`, `Increment`, `IncrementEach`, `Set`, `SetMap`, `SetRow`

### `db.DeleteBuilder` (119)

Started with `DB.Delete(table)`. Guarded like `UpdateBuilder`. No grouping, `Offset`, unions or locks. CTEs are written before `DELETE` (not on MariaDB).

**Build SQL** (1)

`ToSQL`

**Select and source** (1)

`Prefix`

**Where**: the same `Where*` / `OrWhere*` methods as `SelectBuilder` (64), including `WhereGroup` and `WhereExists`.

**JSON conditions**: the same `WhereJson*` / `OrWhereJson*` methods as `SelectBuilder` (14).

**Joins** (20)

`CrossJoin`, `CrossJoinSub`, `Join`, `JoinFunc`, `JoinLateral`, `JoinSub`, `JoinSubFunc`,
`JoinWhere`, `LeftJoin`, `LeftJoinFunc`, `LeftJoinLateral`, `LeftJoinSub`, `LeftJoinSubFunc`,
`LeftJoinWhere`, `RightJoin`, `RightJoinFunc`, `RightJoinSub`, `RightJoinSubFunc`, `RightJoinWhere`,
`StraightJoin`

**Ordering and paging** (10)

`InRandomOrder`, `Latest`, `Limit`, `Oldest`, `OrderBy`, `OrderByDesc`, `OrderByRaw`, `Reorder`,
`ReorderBy`, `Take`

**Common table expressions** (3)

`With`, `WithRaw`, `WithRecursive`

**Control flow** (4)

`AllRows`, `Clone`, `Err`, `Returning`

**Write** (2)

`Exec`, `Get`

### `db.WhereClause` (78)

Every `Where*` and `OrWhere*` method of the select builder, including `WhereGroup`, `OrWhereGroup`, `WhereExists` and the JSON conditions. A group callback receives this type.

### `db.HavingClause` (14)

`Having`, `HavingBetween`, `HavingGroup`, `HavingNotBetween`, `HavingNotNull`, `HavingNull`,
`HavingRaw`, `OrHaving`, `OrHavingBetween`, `OrHavingGroup`, `OrHavingNotBetween`,
`OrHavingNotNull`, `OrHavingNull`, `OrHavingRaw`

### `db.JoinClause` (16)

`On`, `OnGroup`, `OrOn`, `OrOnGroup`, `OrWhere`, `OrWhereIn`, `OrWhereNotIn`, `OrWhereNotNull`,
`OrWhereNull`, `OrWhereRaw`, `Where`, `WhereIn`, `WhereNotIn`, `WhereNotNull`, `WhereNull`,
`WhereRaw`

### `db.Query[T]` (169)

Select-only. Every `SelectBuilder` chain method is mirrored and returns `*db.Query[T]`, so `T` survives the chain; the readers return `T` values (see [Typed API](typed-api.md)). `Apply` and `Builder` give access to the underlying `*db.SelectBuilder`. `Min`, `Max`, `Value`, `Pluck` and `Aggregate` are not mirrored: use `db.ValueAs` and `db.PluckAs`, or the `SelectBuilder` readers.

**Select and source** (8)

`AddSelect`, `Distinct`, `From`, `FromRaw`, `FromSub`, `Select`, `SelectRaw`, `SelectSub`

**Where**: the same `Where*` / `OrWhere*` methods as `SelectBuilder` (64), including `WhereGroup` and `WhereExists`.

**JSON conditions**: the same `WhereJson*` / `OrWhereJson*` methods as `SelectBuilder` (14).

**Group and having** (16)

`GroupBy`, `GroupByRaw`, `Having`, `HavingBetween`, `HavingGroup`, `HavingNotBetween`,
`HavingNotNull`, `HavingNull`, `HavingRaw`, `OrHaving`, `OrHavingBetween`, `OrHavingGroup`,
`OrHavingNotBetween`, `OrHavingNotNull`, `OrHavingNull`, `OrHavingRaw`

**Joins** (20)

`CrossJoin`, `CrossJoinSub`, `Join`, `JoinFunc`, `JoinLateral`, `JoinSub`, `JoinSubFunc`,
`JoinWhere`, `LeftJoin`, `LeftJoinFunc`, `LeftJoinLateral`, `LeftJoinSub`, `LeftJoinSubFunc`,
`LeftJoinWhere`, `RightJoin`, `RightJoinFunc`, `RightJoinSub`, `RightJoinSubFunc`, `RightJoinWhere`,
`StraightJoin`

**Ordering and paging** (15)

`ForPage`, `ForPageAfterID`, `ForPageBeforeID`, `InRandomOrder`, `Latest`, `Limit`, `Offset`,
`Oldest`, `OrderBy`, `OrderByDesc`, `OrderByRaw`, `Reorder`, `ReorderBy`, `Skip`, `Take`

**Common table expressions** (3)

`With`, `WithRaw`, `WithRecursive`

**Unions, locks, index hints** (8)

`ForceIndex`, `IgnoreIndex`, `Lock`, `LockForUpdate`, `SharedLock`, `Union`, `UnionAll`, `UseIndex`

**Control flow** (5)

`Apply`, `Builder`, `Clone`, `Unless`, `When`

**Read** (16)

`Avg`, `Chunk`, `ChunkByID`, `Count`, `CursorPaginate`, `DoesntExist`, `Each`, `Exists`, `Find`,
`First`, `Get`, `Paginate`, `SimplePaginate`, `Sole`, `Stream`, `Sum`

