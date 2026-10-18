# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`quill`: a fluent SQL query builder for Go (not an ORM) for PostgreSQL, MySQL, MariaDB and SQLite. The module is
`github.com/akfaiz/quill`; the **public package is `db`** (`github.com/akfaiz/quill/db`), so callers write
`db.Use(db.Postgres).Table("users")`. The root module is **standard library only at runtime**: keep it that way. The only third-party dependencies are
`github.com/stretchr/testify` and `github.com/DATA-DOG/go-sqlmock`, used by tests alone (`require`/`assert`, mocked `*sql.DB` via the `newMock` helper in `db/mock_test.go`); never import them from non-test code.

## Commands

```sh
go build ./... && go vet ./... && golangci-lint run ./... && go test -race ./...   # root module (db + internal/*)
go test -run TestName ./db                      # one test; subtests: -run 'TestName/case_name/postgres'
go test -run=NONE -fuzz=FuzzDecode -fuzztime=20s ./internal/keyset   # fuzz targets are listed in CONTRIBUTING.md
```

End-to-end tests are a **separate module** (`./...` from the root does not include them):

```sh
cd test/integration
go test -race ./...                 # SQLite + PostgreSQL 16 + MySQL 8.4 + MariaDB 11.4 (Docker via testcontainers)
go test -run TestPostgres ./...     # one dialect; also TestMySQL, TestMariaDB, TestSQLite. Only that container starts
GOQUERY_SKIP_CONTAINERS=1 go test ./...   # skip the Docker-backed suites
```

- Both modules target Go 1.25. In `test/integration`, `modernc.org/sqlite` is pinned to v1.59.0 (newer needs Go 1.26);
  after touching dependencies run `GOTOOLCHAIN=local go mod tidy` so a newer toolchain cannot bump the `go` line.
- A fuzz failure writes `testdata/fuzz/...`: fix the cause and commit that file; it becomes a regression case.

## Architecture

**Compile pipeline (read several files to see it).** `Builder` (`db/builder.go`) holds mutable clause state; the
terminal calls (`ToSQL`, `Get`, `Insert`, ...) build a `compiler` (`db/compiler.go`) that appends SQL text and bind args in
order via `c.param`/`c.value`, so `$n` numbering stays correct across nested subqueries. SELECT is compiled in
`db/grammar.go`, writes in `db/write.go`, JSON in `db/json.go`, joins in `db/join.go`. Executors live on `Builder`
(`read.go`, `exec_write.go`, `paginate.go`, `chunk.go`) and run through `DB.runSQL` in `db.go`.
Note: `db/grammar.go` is the SELECT compiler, **not** the per-dialect grammar.

**Dialects.** `db.Dialect` selects an `internal/dialect.Grammar` (one file per grammar) for single-token differences
(quoting, placeholders, LIKE, dates, locks, index hints). Structural differences (update/delete with joins, upsert,
truncate, RETURNING, JSON updates) are `switch c.d` branches in `write.go` and `json.go`. MariaDB is a MySQL-family
variant: use `c.d.mysqlFamily()` / `case MySQL, MariaDB:` where MySQL is checked, and branch on `MariaDB` only where it
really differs (no lateral joins; `json_query` instead of `CAST AS JSON`).

**Two entry points.** `db.Use(dialect)` builds SQL with no connection (running a query returns `ErrNotBound`, never a
panic); `db.New(conn, dialect)` also executes. Package-level `db.Table` no longer exists: start from `DB.Table`.

**Typed API.** `Query[T]` (`typed*.go`) wraps a Builder because Go methods cannot be generic. `typed_chain.go` mirrors a
curated set of Builder methods (a parity test fails if one is added without a case); everything else goes through
`Apply`. `From[T]` takes the table from `T.TableName()` (`tablename.go`: zero value, cached per type, panics become
`ErrInvalidArgs`). Struct writes use `Row`/`Rows` (`structs.go`) and the `db:"name,omitempty"` tags.

**`internal/` packages** exist because they cannot import `db` (no cycles) and are unit-tested alone: `dialect`,
`rowmap` (rows <-> structs/maps/scalars), `jsonpath`, `keyset` (decodes untrusted cursors), `rawsql` (`ToRawSQL`
interpolation), `errs` (shared sentinel errors, re-exported by `db`).

**Behavioural invariants to preserve**
- Values are always bound, identifiers always quoted; operators and order directions are allow-listed.
- A bad argument never panics: the builder records the first error (`b.fail`) and returns it at the terminal call. Only
  the `Must*` functions panic.
- Subqueries are snapshotted when added (`snapshot`, `Clone`); mutating the original afterwards must not change the query.
- `Update`/`Delete`/`Increment*` without a WHERE or LIMIT return `ErrNoWhere` unless `AllRows()` was called.
- Unsupported-on-this-dialect returns `ErrUnsupported` instead of emitting SQL that silently differs.

## Testing conventions

- Assert with testify: `require` for steps later code depends on (`require.NoError`), `assert` otherwise; expected value first.
- **Golden tests run every dialect.** The runners (`golden`, `stmtCase`, `perCase`) take `pg`, `my`, `lite` expectations
  and an optional `maria` override; empty means "same as MySQL", which is how MariaDB inheritance is proven. A SQL change
  needs expectations for all dialects. Tests use `db.Use(...)`, no database.
- Test files are named after the source file they cover (`where.go` has `where_test.go`), never after a milestone.
- Each database has its own independent file in `test/integration` with dialect-specific expectations inline (no
  dialect branching); a golden test proves the text, only a real server proves it is valid.
- Probe the real server before copying another implementation's choice (MariaDB `json_value` returns NULL for
  arrays, so it is deliberately not used).
- Never name a local variable `db` (it hides the package); use `conn`.

## Docs

User guides and the method reference live in the GitHub wiki (https://github.com/akfaiz/quill/wiki), not in this
repo; there is no `docs/` folder. Keep `go doc` comments accurate and `db/example_test.go` examples compiling.
