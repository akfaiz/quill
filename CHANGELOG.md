# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-10-17

### Breaking

The single `*db.Builder` is split into one builder per statement, each exposing only the methods valid for it.
Writes finish with `Exec`, `Get` (scans `RETURNING` rows) or `ToSQL`.

- Entry points: `DB.Select(cols...)` + `From`, `DB.Insert(table)`, `DB.Update(table)`, `DB.Delete(table)`.
  `Truncate`, `TruncateSQL` and `UpdateOrInsert` move to `*db.DB`.
- Removed: `Table`, `NewQuery`, `NewBuilder`, `db.Builder`, all `...SQL` and `...Returning` methods, the typed writes
  on `Query[T]` (now select-only), and `db.Rows`, `db.AggregateAs`, `db.MinAs`, `db.MaxAs`.
- Subquery arguments take `*db.SelectBuilder`; `WhereGroup` / `HavingGroup` callbacks take `*db.WhereClause` /
  `*db.HavingClause`; `When`, `Unless`, `Tap` take `*db.SelectBuilder`.
- An update or delete no longer inherits select-only state (grouping, unions, locks); CTEs are supported again on
  them (see Added)

CTE methods take options instead of one method per combination:

- `With(name, q, cols...)` is `With(name, q, db.CTEColumns(cols...))`; `WithRaw` likewise.
- `WithRecursive(name, anchor.UnionAll(step), cols...)` is `WithRecursive(name, anchor, step, db.CTEColumns(cols...))`;
  `db.CTEUnionDistinct()` gives `UNION`. A recursive CTE no longer accepts an anchor that already has unions.
- `WithMaterialized(...)` / `WithNotMaterialized(...)` are `With(..., db.CTEMaterialized())` /
  `With(..., db.CTENotMaterialized())`.

| Before | After |
| --- | --- |
| `conn.Table("t").Where(...).Get(ctx, &x)` | `conn.Select().From("t").Where(...).Get(ctx, &x)` |
| `db.NewBuilder(d)`, `conn.NewQuery()` | `db.Use(d).Select()`, `conn.Select()` |
| `Insert(ctx, m)` | `conn.Insert("t").Values(m).Exec(ctx)` |
| `InsertGetID` / `InsertOrIgnore` | `.ExecGetID(ctx)` / `.Ignore()` on `Insert` |
| `InsertUsing(ctx, cols, sub)` | `conn.Insert("t").Using(cols, sub).Exec(ctx)` |
| `Upsert(ctx, rows, uniqueBy, update)` | `conn.Insert("t").Values(rows...).Upsert(uniqueBy, update).Exec(ctx)` |
| `Where(...).Update(ctx, m)` | `conn.Update("t").SetMap(m).Where(...).Exec(ctx)` |
| `UpdateFrom(ctx, m)` | `conn.Update("t").Join(...).FromJoins().SetMap(m).Where(...).Exec(ctx)` |
| `Increment(ctx, c, n, extra)` | `conn.Update("t").Increment(c, n).SetMap(extra).Where(...).Exec(ctx)` |
| `Where(...).Delete(ctx)` | `conn.Delete("t").Where(...).Exec(ctx)` |
| `InsertReturning` / `UpdateReturning` / `DeleteReturning` | `.Returning(cols...).Get(ctx, &out)` on the write builder |
| `InsertSQL`, `UpdateSQL`, `DeleteSQL`, ... | `ToSQL()` on the write builder (`IDSQL()` for `InsertGetIDSQL`) |
| `conn.Table("t").Truncate(ctx)` | `conn.Truncate(ctx, "t")` |
| `conn.Table("t").UpdateOrInsert(ctx, a, v)` | `conn.UpdateOrInsert(ctx, "t", a, v)` |
| typed `Insert` / `Update(ctx, u)` | `conn.Insert("t").Rows(u)` / `conn.Update("t").SetRow(u)` |

### Added

- CTE options: `db.CTEColumns`, `db.CTEMaterialized`, `db.CTENotMaterialized`, `db.CTEUnionDistinct`. They combine
  freely, e.g. a raw, materialized, column-named CTE.
- `With`, `WithRaw` and `WithRecursive` on `UpdateBuilder` and `DeleteBuilder` (`WITH ... UPDATE` / `DELETE` on
  PostgreSQL, MySQL 8, SQLite; MariaDB returns `ErrUnsupported`) and on `InsertBuilder` (moved into the `Using` select).
- The `MATERIALIZED` hint works on SQLite 3.35+ as well as PostgreSQL.
- `db.SelectBuilder`, `InsertBuilder`, `UpdateBuilder`, `DeleteBuilder`, `WhereClause`, `HavingClause`.
- `UpdateBuilder`: `Set`, `SetMap`, `SetRow`, `Increment`, `Decrement`, `IncrementEach`, `DecrementEach`, `FromJoins`.
- `InsertBuilder`: `Values`, `Rows`, `Ignore`, `Using`, `Upsert`, `ExecGetID`, `IDSQL`.
- `internal/chaingen` generates the builders' chain methods into `db/chain_gen.go` (`go generate ./db`).
- `BenchmarkRun` in `test/benchmark`: executing and scanning queries on SQLite vs sqlx, GORM and `database/sql`.

### Changed

- Scanning into structs and scalars is about 4x faster (100 rows, 4 fields: ~1,500 to ~20 allocations) by reusing
  scan targets across rows.
- Fewer allocations when running queries: `Find`, `First` and `Sole` no longer deep-clone the builder, and the
  insert, update and delete compilers pre-size their buffers and args (3-row insert: 24 to 14 allocs/op).
- More tests; CI, Makefile and benchmark docs updated (library order: quill, sqlbuilder, goqu, squirrel).

### Fixed

- Predicates the library appends (`ForPageAfterID`, `ForPageBeforeID`, `ChunkByID`, update/delete helpers) are now
  grouped, so an existing `OR` condition no longer changes their meaning.
- MySQL/MariaDB joined `UPDATE`/`DELETE` with `ORDER BY`/`LIMIT` now returns `ErrUnsupported` instead of silently
  dropping the clauses.
- Nil subquery arguments (subqueries, joins, unions) record an error instead of panicking.
- `WhereIn` and `WhereRowValues` copy the caller's slice, so later mutation no longer alters the query.
- `ChunkByID` no longer panics or reads the wrong cursor when the callback truncates or reorders the batch, and it
  orders by its key.
- `ChunkByID` with a NULL pointer key no longer fails.
- Keyset paging (`ChunkByID`, `CursorPaginate`) on unions is rejected instead of filtering only the first branch.
- A panic or `runtime.Goexit` inside a transaction callback is handled safely.
- SQLite `Truncate` works on tables without `AUTOINCREMENT`.
- SQLite columns prefix the table segment correctly.
- SQLite JSON path updates use `JSON_SET`, matching the other dialects.

## [0.1.0] - 2026-10-10

### Added

- Initial release: fluent SQL query builder for PostgreSQL, MySQL, MariaDB and SQLite.

[0.2.0]: https://github.com/akfaiz/quill/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/akfaiz/quill/releases/tag/v0.1.0
