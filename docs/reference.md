# Reference

- [Errors](#errors) · [Types](#types) · [Options](#options) · [Package functions](#package-functions)
- [Method index](#method-index)

For signatures and doc comments use `go doc github.com/akfaiz/quill/db` (add a name, for example
`go doc github.com/akfaiz/quill/db Builder.Where`).

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
| `db.ErrNoValues` | A write had nothing to write (all fields omitted, an empty map) |
| `db.ErrNoWhere` | `Update`, `Delete` or `Increment*` without a `WHERE` or `LIMIT` and without `AllRows()` |

Builder errors are collected while you chain and returned by the terminal call; `Err()` shows the
first one early.

## Types

| Type | Purpose |
| --- | --- |
| `db.Dialect` | `Postgres`, `MySQL`, `MariaDB`, `SQLite` |
| `db.DB` | A dialect plus an optional connection. `db.New` or `db.Use` creates it |
| `db.Conn` | `QueryContext` and `ExecContext`: satisfied by `*sql.DB`, `*sql.Tx`, `*sql.Conn` |
| `db.Builder` | The fluent query builder |
| `db.JoinClause` | Conditions of one join, inside `JoinFunc` and friends |
| `db.Expression` | Raw SQL plus arguments; create it with `db.Raw` |
| `db.Query[T]` | The typed query (`db.From[T]`, `db.As[T]`) |
| `db.Tabler` | `TableName() string`, implemented by row types |
| `db.Option` | A `db.New` / `db.Use` option |
| `db.QueryEvent` | `SQL`, `NumArgs`, `Args`, `Duration`, `Err`: passed to hooks |
| `db.Stmt` | `SQL` and `Args` of one statement (`TruncateSQL`) |
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

## Package functions

`AggregateAs`, `As`, `From`, `MaxAs`, `MinAs`, `New`, `NewBuilder`, `PluckAs`, `Raw`, `Row`, `Rows`,
`Stream`, `TableName`, `Use`, `ValueAs`, `WithHook`, `WithLogArgs`, `WithPrefix`

`db.NewBuilder(dialect)` returns a blank builder with no `DB`, for low-level use; prefer
`db.Use(dialect).NewQuery()`.

## Method index

Every exported method, grouped by purpose. The guides explain each group.

## `db.Builder`

### Build SQL (6)

`AggregateSQL`, `ExistsSQL`, `MustRawSQL`, `MustSQL`, `ToRawSQL`, `ToSQL`

### Write SQL (build only) (14)

`DecrementEachSQL`, `DecrementSQL`, `DeleteSQL`, `IncrementEachSQL`, `IncrementSQL`,
`InsertGetIDSQL`, `InsertOrIgnoreSQL`, `InsertOrIgnoreUsingSQL`, `InsertSQL`, `InsertUsingSQL`,
`TruncateSQL`, `UpdateFromSQL`, `UpdateSQL`, `UpsertSQL`

### Select and source (12)

`AddSelect`, `Clone`, `Distinct`, `Err`, `From`, `FromRaw`, `FromSub`, `NewQuery`, `Prefix`,
`Select`, `SelectRaw`, `SelectSub`

### JSON conditions (14)

`OrWhereJsonContains`, `OrWhereJsonContainsKey`, `OrWhereJsonDoesntContain`,
`OrWhereJsonDoesntContainKey`, `OrWhereJsonDoesntOverlap`, `OrWhereJsonLength`,
`OrWhereJsonOverlaps`, `WhereJsonContains`, `WhereJsonContainsKey`, `WhereJsonDoesntContain`,
`WhereJsonDoesntContainKey`, `WhereJsonDoesntOverlap`, `WhereJsonLength`, `WhereJsonOverlaps`

### Where (64)

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

### Group and having (16)

`GroupBy`, `GroupByRaw`, `Having`, `HavingBetween`, `HavingGroup`, `HavingNotBetween`,
`HavingNotNull`, `HavingNull`, `HavingRaw`, `OrHaving`, `OrHavingBetween`, `OrHavingGroup`,
`OrHavingNotBetween`, `OrHavingNotNull`, `OrHavingNull`, `OrHavingRaw`

### Joins (20)

`CrossJoin`, `CrossJoinSub`, `Join`, `JoinFunc`, `JoinLateral`, `JoinSub`, `JoinSubFunc`,
`JoinWhere`, `LeftJoin`, `LeftJoinFunc`, `LeftJoinLateral`, `LeftJoinSub`, `LeftJoinSubFunc`,
`LeftJoinWhere`, `RightJoin`, `RightJoinFunc`, `RightJoinSub`, `RightJoinSubFunc`, `RightJoinWhere`,
`StraightJoin`

### Ordering and paging (15)

`ForPage`, `ForPageAfterID`, `ForPageBeforeID`, `InRandomOrder`, `Latest`, `Limit`, `Offset`,
`Oldest`, `OrderBy`, `OrderByDesc`, `OrderByRaw`, `Reorder`, `ReorderBy`, `Skip`, `Take`

### Unions, locks, index hints (8)

`ForceIndex`, `IgnoreIndex`, `Lock`, `LockForUpdate`, `SharedLock`, `Union`, `UnionAll`, `UseIndex`

### Control flow (5)

`AllRows`, `Returning`, `Tap`, `Unless`, `When`

### Read (21)

`Aggregate`, `Avg`, `Chunk`, `ChunkByID`, `Count`, `CursorPaginate`, `DoesntExist`, `Each`,
`Exists`, `Find`, `First`, `Get`, `Max`, `Min`, `Paginate`, `Pluck`, `PluckMap`, `SimplePaginate`,
`Sole`, `Sum`, `Value`

### Write (18)

`Decrement`, `DecrementEach`, `Delete`, `DeleteReturning`, `Increment`, `IncrementEach`, `Insert`,
`InsertGetID`, `InsertOrIgnore`, `InsertOrIgnoreUsing`, `InsertReturning`, `InsertUsing`,
`Truncate`, `Update`, `UpdateFrom`, `UpdateOrInsert`, `UpdateReturning`, `Upsert`

### `db.DB` (7)

`Dialect`, `Exec`, `NewQuery`, `Query`, `Table`, `Transaction`, `TransactionOpts`

### `db.JoinClause` (16)

`On`, `OnGroup`, `OrOn`, `OrOnGroup`, `OrWhere`, `OrWhereIn`, `OrWhereNotIn`, `OrWhereNotNull`,
`OrWhereNull`, `OrWhereRaw`, `Where`, `WhereIn`, `WhereNotIn`, `WhereNotNull`, `WhereNull`,
`WhereRaw`

### `db.Query[T]` (75)

`AddSelect`, `AllRows`, `Apply`, `Avg`, `Builder`, `Chunk`, `ChunkByID`, `Clone`, `Count`,
`CursorPaginate`, `Delete`, `DeleteReturning`, `Distinct`, `DoesntExist`, `Each`, `Exists`, `Find`,
`First`, `ForPage`, `Get`, `GroupBy`, `Having`, `HavingRaw`, `Insert`, `InsertGetID`,
`InsertOrIgnore`, `InsertReturning`, `Join`, `JoinFunc`, `Latest`, `LeftJoin`, `LeftJoinFunc`,
`Limit`, `LockForUpdate`, `Offset`, `Oldest`, `OrWhere`, `OrWhereGroup`, `OrWhereIn`, `OrWhereNull`,
`OrWhereRaw`, `OrderBy`, `OrderByDesc`, `OrderByRaw`, `Paginate`, `Returning`, `RightJoin`,
`Select`, `SelectRaw`, `SharedLock`, `SimplePaginate`, `Sole`, `Stream`, `Sum`, `Truncate`,
`Unless`, `Update`, `UpdateMap`, `UpdateReturning`, `Upsert`, `When`, `Where`, `WhereBetween`,
`WhereColumn`, `WhereExists`, `WhereGroup`, `WhereIn`, `WhereLike`, `WhereNot`, `WhereNotBetween`,
`WhereNotExists`, `WhereNotIn`, `WhereNotNull`, `WhereNull`, `WhereRaw`

The typed chain methods forward to the builder method of the same name; see
[Typed API](typed-api.md#the-typed-chain).
