# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1] - [Unreleased]

### Added

- `BenchmarkRun` in `test/benchmark`: executing and scanning queries on SQLite vs sqlx, GORM and `database/sql`.

### Changed

- Scanning into structs and scalars is about 4x faster (100 rows, 4 fields: ~1,500 to ~20 allocations) by reusing
  scan targets across rows.
- Fewer allocations when running queries: `Find`, `First` and `Sole` no longer deep-clone the builder, and
  `InsertSQL`, `UpdateSQL` and `DeleteSQL` pre-size their buffers and args (3-row insert: 24 to 14 allocs/op).
- More tests; CI, Makefile and benchmark docs updated (library order: quill, sqlbuilder, goqu, squirrel).

### Fixed

- Predicates the library appends (`ForPageAfterID`, `ForPageBeforeID`, `ChunkByID`, update/delete helpers) are now
  grouped, so an existing `OR` condition no longer changes their meaning.
- MySQL/MariaDB joined `UPDATE`/`DELETE` with `ORDER BY`/`LIMIT` now returns `ErrUnsupported` instead of silently
  dropping the clauses.
- Nil `*Builder` arguments (subqueries, joins, unions) record an error instead of panicking.
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

[0.1.1]: https://github.com/akfaiz/quill/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/akfaiz/quill/releases/tag/v0.1.0
