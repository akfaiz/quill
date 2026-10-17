# Library comparison benchmarks

Two benchmarks compare quill with other popular Go libraries:

| Benchmark | What it times | Compared with |
| --- | --- | --- |
| `BenchmarkBuild` | Building SQL, no database | [go-sqlbuilder](https://github.com/huandu/go-sqlbuilder) v1.44.0, [goqu](https://github.com/doug-martin/goqu) v9.19.0, [squirrel](https://github.com/Masterminds/squirrel) v1.5.4 |
| `BenchmarkRun` | Build + execute + scan, in-memory SQLite | [sqlx](https://github.com/jmoiron/sqlx), [GORM](https://gorm.io), [bun](https://bun.uptrace.dev), plain `database/sql` |

This is a separate module, so the root module stays free of third-party dependencies.

```sh
make bench-libs                 # from the repo root: 6 runs per benchmark, saved to results.txt
make bench-libs-update          # rewrite both Results tables below from results.txt (no re-run)
cd test/benchmark && go test -v -run Comparable .   # guard tests; -v prints the SQL each library builds
```

## Building SQL

### What is measured

Only **building** a query: from the first builder call to the `(sql, args)` pair. No
database, no network, no execution. Every scenario targets PostgreSQL with bound `$n`
placeholders, which is what an application actually sends.

`TestScenariosAreComparable` runs first in spirit: every library must build each query
without error and bind the same number of arguments, so a benchmark cannot win by quietly
building a different query. goqu and sqlbuilder bind `LIMIT` and `OFFSET` as parameters
(quill and squirrel inline them as literals), and the test accounts for that explicitly.

### Results

<!-- bench:start -->
Apple M1 Pro, Go 1.25.14, median of 6 runs (`make bench-libs`, then `make bench-libs-update`). Lower is
better; the last column is time relative to quill.

| Scenario | Library | ns/op | B/op | allocs/op | Time vs quill |
| --- | --- | ---: | ---: | ---: | ---: |
| SimpleSelect | quill | 786 | 864 | 8 | 1.0x |
| | sqlbuilder | 1,364 | 1,488 | 37 | 1.7x |
| | goqu | 2,997 | 3,832 | 77 | 3.8x |
| | squirrel | 4,510 | 3,882 | 69 | 5.7x |
| ComplexSelect | quill | 3,023 | 2,968 | 21 | 1.0x |
| | sqlbuilder | 3,018 | 3,866 | 60 | 1.0x |
| | goqu | 10,375 | 11,328 | 329 | 3.4x |
| | squirrel | 12,011 | 10,903 | 180 | 4.0x |
| WideWhere (12 predicates) | quill | 2,572 | 4,920 | 13 | 1.0x |
| | sqlbuilder | 3,392 | 3,794 | 57 | 1.3x |
| | goqu | 6,946 | 7,024 | 214 | 2.7x |
| | squirrel | 14,899 | 12,993 | 230 | 5.8x |
| Insert3Rows | quill | 804 | 968 | 6 | 1.0x |
| | sqlbuilder | 2,283 | 2,169 | 41 | 2.8x |
| | goqu | 3,406 | 3,944 | 105 | 4.2x |
| | squirrel | 5,570 | 4,507 | 93 | 6.9x |
| Update | quill | 747 | 976 | 8 | 1.0x |
| | sqlbuilder | 1,580 | 1,400 | 41 | 2.1x |
| | goqu | 2,529 | 2,616 | 71 | 3.4x |
| | squirrel | 3,994 | 3,218 | 65 | 5.3x |
| Delete | quill | 385 | 720 | 5 | 1.0x |
| | sqlbuilder | 853 | 936 | 28 | 2.2x |
| | goqu | 1,157 | 1,284 | 37 | 3.0x |
| | squirrel | 1,993 | 1,809 | 34 | 5.2x |
<!-- bench:end -->

sqlbuilder is the closest competitor. On ComplexSelect it is level with quill (about 1.0x;
run-to-run noise is around 5-10%), even though quill makes about a third as many allocations.
On WideWhere quill allocates more bytes than it does.

Absolute numbers depend on the machine; compare the ratios, and re-run on your own
hardware before quoting them.

## Running queries

### What is measured

`BenchmarkRun` measures the full round trip: build the query, execute it and scan the result
into a struct, against an in-memory SQLite database (`modernc.org/sqlite`, pure Go, no Docker).
Every library shares the same single connection and the same 1,000 seeded rows, so engine cost is
identical and only library overhead differs. Libraries: quill, [sqlx](https://github.com/jmoiron/sqlx),
[GORM](https://gorm.io), [bun](https://bun.uptrace.dev) and a hand-written `database/sql` loop (`stdlib`) as the floor.

`TestRunScenariosAreComparable` checks every library returns the same result (same id, id sum,
count or rows affected) before any timing counts. Because SQLite time is shared, ratios here are
smaller than in the build-only table; network and disk latency would shrink them further.

### Results

<!-- bench-run:start -->
Apple M1 Pro, Go 1.25.14, median of 6 runs (`make bench-libs`, then `make bench-libs-update`). Lower is
better; the last column is time relative to quill.

| Scenario | Library | ns/op | B/op | allocs/op | Time vs quill |
| --- | --- | ---: | ---: | ---: | ---: |
| SelectOne | quill | 7,812 | 2,232 | 35 | 1.0x |
| | sqlx | 6,409 | 864 | 28 | 0.8x |
| | gorm | 10,205 | 4,905 | 72 | 1.3x |
| | bun | 8,737 | 5,824 | 31 | 1.1x |
| | stdlib | 5,774 | 624 | 23 | 0.7x |
| SelectMany | quill | 75,261 | 24,808 | 637 | 1.0x |
| | sqlx | 94,652 | 30,910 | 827 | 1.3x |
| | gorm | 120,176 | 39,723 | 1,271 | 1.6x |
| | bun | 87,195 | 31,173 | 734 | 1.2x |
| | stdlib | 75,694 | 12,517 | 714 | 1.0x |
| Count | quill | 61,968 | 1,984 | 27 | 1.0x |
| | sqlx | 63,870 | 480 | 16 | 1.0x |
| | gorm | 65,202 | 3,872 | 47 | 1.1x |
| | bun | 62,831 | 1,304 | 22 | 1.0x |
| | stdlib | 60,889 | 400 | 13 | 1.0x |
| Insert | quill | 6,727 | 1,416 | 17 | 1.0x |
| | sqlx | 5,307 | 256 | 9 | 0.8x |
| | gorm | 21,478 | 7,260 | 87 | 3.2x |
| | bun | 7,484 | 5,220 | 13 | 1.1x |
| | stdlib | 5,404 | 256 | 9 | 0.8x |
| Update | quill | 4,193 | 976 | 12 | 1.0x |
| | sqlx | 3,510 | 160 | 6 | 0.8x |
| | gorm | 12,494 | 6,820 | 69 | 3.0x |
| | bun | 5,322 | 5,004 | 12 | 1.3x |
| | stdlib | 3,302 | 160 | 6 | 0.8x |
<!-- bench-run:end -->


In the run benchmark quill scans many rows as fast as a hand-written `database/sql` loop (SelectMany) and
about 1.3x faster than sqlx, because it reuses its scan targets across rows. Single-row reads are about 1.2x
slower than sqlx and writes 1.2-1.3x slower: the gap is the fixed cost of building the query (clone, clauses,
compiler) that raw SQL strings skip. quill is about 3x faster than GORM on writes. bun is within about 1.3x of quill on every scenario and
allocates 4-5x more bytes per query. Count is dominated by SQLite
itself, so every library ties.

## Reading the numbers fairly

- **Build time is rarely the bottleneck** (see the run table for how the gap shrinks once a database is involved). A query round trip to a database costs
  hundreds of microseconds to milliseconds. Microseconds saved here matter for
  allocation pressure and very hot paths, not for typical request latency.
- **Squirrel and sqlbuilder do less work.** Neither quotes identifiers, so their output
  (`SELECT id FROM users`) is not hardened the way quill's and goqu's are
  (`SELECT "id" FROM "users"`). Quoting is real work, which makes a tie with sqlbuilder
  more favourable to quill than the raw number suggests. Squirrel is the slowest here
  even without it.
- **Query style differs.** sqlbuilder takes conditions as pre-rendered strings
  (`sb.GreaterThan("votes", 100)`) and needs a new builder per query; quill and the others
  chain methods directly.
- **Quill's `Like` is case-insensitive on PostgreSQL** (`ilike`), the others emit
  plain `LIKE`. Quill's insert takes maps and sorts the column names; the others take
  ordered column and value lists.
- **Not covered:** CTEs/unions/JSON (not every library supports them), prepared-statement
  reuse, real network and disk latency, and one-time setup cost such as `goqu.Dialect(...)`.
- Library features differ. This compares speed on queries every library in a table can express, not
  which library is better for your use case.
