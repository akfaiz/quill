# Contributing

## Layout

| Path | Contents |
| --- | --- |
| `db/` | The public package. Builder, compiler, executor, typed API |
| `internal/dialect` | The per-database `Grammar` implementations (quoting, placeholders, LIKE, dates, locks, index hints) |
| `internal/rowmap` | Rows to structs, maps and scalars, and structs back to rows |
| `internal/jsonpath` | `col->a->b[0]` selectors rendered into JSON path literals |
| `internal/keyset` | The cursor token codec (decodes untrusted input) |
| `internal/rawsql` | Argument interpolation for `ToRawSQL` |
| `internal/errs` | Error values shared with `db`, which re-exports them |
| `test/benchmark/` | A separate module: quill vs sqlbuilder, goqu and squirrel (`make bench-libs`) |
| `test/integration/` | A separate module: end-to-end tests on SQLite, PostgreSQL, MySQL and MariaDB |
| `docs/` | The guides |

Why `internal/`: each package is self-contained (no import of `db`), unit-testable on its own,
and hidden from users. Code that works on `Builder`'s unexported state (the compiler, the
`where`/`join`/`write` code, the executor) has to stay in `db/`, because Go cannot define methods
on a type from another package.

The root module has **no third-party runtime dependencies** and should stay that way. The only
exception is `github.com/stretchr/testify`, imported from `_test.go` files alone (`require` for steps
later code depends on, `assert` otherwise). Anything that needs a driver or test infrastructure
belongs in `test/integration/`.

## Running the tests

CI (`.github/workflows/ci.yml`) runs build, vet, lint, unit tests and the integration suites on every
pull request to `main` and every push to `main`; run the same commands below before you push.

```sh
go build ./...
go vet ./...
golangci-lint run ./...
go test -race ./...                  # unit and golden tests, all packages
```

```sh
cd test/integration
go test -race ./...                  # runs on SQLite, PostgreSQL and MySQL
```

### What each layer proves

| Layer | Where | Proves |
| --- | --- | --- |
| Golden SQL | `db/*_test.go` | The exact SQL and arguments for every dialect. Use `db.Use(...)`, no database needed |
| Parity | `db/typed_chain_test.go` | Every typed chain method produces the same SQL as its builder method |
| Unit | `internal/*/..._test.go` | Each internal package in isolation (fake `database/sql` driver for scanning) |
| Examples | `db/example_test.go` | Documentation that is executed and checked |
| Fuzz | see below | Quoting, placeholder and untrusted-input code never misbehave |
| End to end | `test/integration/` | Real databases accept the SQL and return the right rows |

### Integration suites

`test/integration/` has one independent suite per database, each in its own file, so a dialect
can be run, read and changed on its own:

| File | Database | Needs |
| --- | --- | --- |
| `sqlite_test.go` | SQLite, in process (pure-Go driver) | nothing |
| `postgres_test.go` | PostgreSQL 16 | Docker |
| `mysql_test.go` | MySQL 8.4 | Docker |
| `mariadb_test.go` | MariaDB 11.4 | Docker |

Each container-backed file starts its own container on first use through
[testcontainers-go](https://github.com/testcontainers/testcontainers-go), shares it across that
file's tests, and removes it when the run ends. They hold the schema, the seed rows, and every
dialect-specific expectation inline (for example MySQL's straight joins and index hints, or
PostgreSQL's `UpdateFrom` and `RETURNING`), so there is no branching on the dialect.

Run one database:

```sh
cd test/integration
go test -run TestSQLite ./...                            # no container starts
go test -run TestPostgres ./...                          # only the PostgreSQL container starts
go test -run TestMySQL ./...                             # only the MySQL container starts
go test -run TestMariaDB ./...                           # only the MariaDB container starts
```

- Without a reachable Docker daemon the PostgreSQL and MySQL tests **skip**.
- `GOQUERY_SKIP_CONTAINERS=1` skips them on purpose.
- `test/integration/go.mod` targets the same Go as the root module (1.25). The newest SQLite
  driver releases need Go 1.26, so `modernc.org/sqlite` is pinned to v1.59.0, the newest that
  supports 1.25. Raise both together when you raise the Go version. After changing dependencies,
  run `GOTOOLCHAIN=local go mod tidy` so a newer toolchain cannot silently bump the `go` line.
- Shared code: `helpers_test.go` (row types, assertions, the seed rows used by PostgreSQL and
  MySQL) and `containers_test.go` (`TestMain`, the Docker check and container cleanup).

### Benchmarks

`make bench` runs every benchmark with allocation stats (`BENCH=SelectComplex` to filter,
`BENCHTIME=3s` for steadier numbers). The `db` benchmarks build and compile SQL for each dialect
without a database; `internal/*` benchmarks cover scanning, cursors, interpolation and JSON paths.
Compare runs with `benchstat`.

### Fuzzing

```sh
go test -run=NONE -fuzz=FuzzIdentifierQuoting         -fuzztime=20s ./db
go test -run=NONE -fuzz=FuzzRawPlaceholders           -fuzztime=20s ./db
go test -run=NONE -fuzz=FuzzQuoteCannotBeEscaped      -fuzztime=20s ./internal/dialect
go test -run=NONE -fuzz=FuzzPathCannotEscapeItsLiteral -fuzztime=20s ./internal/jsonpath
go test -run=NONE -fuzz=FuzzPgAttributesQuoting       -fuzztime=20s ./internal/jsonpath
go test -run=NONE -fuzz=FuzzInterpolateNeverPanics    -fuzztime=20s ./internal/rawsql
go test -run=NONE -fuzz=FuzzDecode                    -fuzztime=20s ./internal/keyset
```

A failing input is written under `testdata/fuzz/`. Fix the cause, then **commit that file**: it
runs as a regression case in a plain `go test`.

## Conventions

- **Every SQL change gets a golden test for all three dialects**, in the file named after the
  source file it covers (`where.go` has `where_test.go`).
- **Behaviour that depends on the database also gets an integration test.** A golden test proves
  the text; only a real server proves it is valid and means what you think.
- **Errors are sentinels** (`db.ErrNoWhere`, ...), wrapped with `%w` and context. Return them;
  do not panic on bad input. The only panics are the `Must...` functions.
- **Values are always bound.** Never concatenate a value into SQL. Identifiers always go through
  the quoting helpers.
- **Be explicit about dialect gaps:** return `db.ErrUnsupported` rather than emit SQL that
  silently differs.
- Format with `gofmt`, keep `golangci-lint` clean, and write doc comments on exported names.
- Test files are named after what they test, not after a milestone.

## Adding a builder method

1. Add the method on `Builder` (record bad arguments with `b.fail(err)`, do not panic).
2. Add the clause or state, and compile it in `db/grammar.go` (or `write.go`, `join.go`,
   `json.go`). Write arguments through `c.param` / `c.value` so numbering stays right.
3. Add a golden test for PostgreSQL, MySQL and SQLite, plus an error test.
4. If it is commonly chained, add it to `typed_chain.go` and the parity table in
   `typed_chain_test.go`. The parity test fails if you forget.
5. Add it to the relevant guide in `docs/`. `docs/reference.md` lists every method, so add it there
   as well.
6. If a database could reject or reinterpret the SQL, add an integration case.

## Adding a dialect

1. Implement `dialect.Grammar` in `internal/dialect/<name>.go` and add a constructor next to
   `Postgres()`, `MySQL()`, `SQLite()` and `MariaDB()`. `internal/dialect/dialect_test.go` shows the
   matrix of behaviours to cover.
2. Add the constant to `Dialect` in `db/dialect.go`, its `String()` case, and its branch in
   `Dialect.grammar()`.
3. The structural differences that are not single tokens (update and delete with joins, upsert,
   truncate, `RETURNING`, JSON updates) are `switch` statements on the dialect in `db/write.go` and
   `db/json.go`. Add a case for the new dialect, or return `db.ErrUnsupported`.
4. Add golden cases for the new dialect to every table-driven test (they list one expected string
   per dialect), then a `test/integration/<dialect>_test.go` with its container, schema and suite
   (copy `postgres_test.go` or `mysql_test.go`).
5. Document it in `docs/dialects.md`.

### A variant of an existing dialect

When a database is mostly another one (MariaDB is mostly MySQL), do not copy the grammar:

- Embed the parent grammar and override only what differs (`internal/dialect/mariadb.go` embeds
  `mysqlGrammar` and overrides `SupportsLateralJoin`).
- Treat the variant as part of the family wherever the parent is checked: `Dialect.mysqlFamily()`
  replaces `== MySQL`, and `case MySQL, MariaDB:` replaces `case MySQL:`. Branch on the variant
  itself only at the points that really differ.
- In the golden runners, give the variant's expectation as an **override** (`maria` in `golden` and
  `stmtCase`, `maria` in `perCase`). When it is empty the runner expects the parent's SQL, so every
  existing case proves the inheritance.
- Add a test that compares the two dialects directly (`db/mariadb_test.go`), and one at grammar
  level that every inherited method agrees.
- **Probe the real server before copying another implementation's choices.** A grammar written
  for a different project records what that project chose, not what is required: MariaDB's
  `json_value` returns `NULL` for arrays and objects, and `json_unquote(json_extract(...))` already
  works there, so quill keeps the MySQL form.

## Documentation

Examples in the guides are written to be correct as shown. When you change behaviour, update the
guide and `docs/reference.md`, and keep `go doc` comments accurate. Examples in
`db/example_test.go` are executed by `go test`.
