# Library comparison benchmarks

Two benchmarks compare quill with other popular Go libraries:

| Benchmark | What it times | Compared with |
| --- | --- | --- |
| `BenchmarkBuild` | Building SQL, no database | [go-sqlbuilder](https://github.com/huandu/go-sqlbuilder) v1.44.0, [goqu](https://github.com/doug-martin/goqu) v9.19.0, [squirrel](https://github.com/Masterminds/squirrel) v1.5.4 |
| `BenchmarkRun` | Build + execute + scan, in-memory SQLite | [sqlx](https://github.com/jmoiron/sqlx), [GORM](https://gorm.io), plain `database/sql` |

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
| SimpleSelect | quill | 641 | 864 | 8 | 1.0x |
| | sqlbuilder | 1,334 | 1,488 | 37 | 2.1x |
| | goqu | 2,881 | 3,832 | 77 | 4.5x |
| | squirrel | 4,502 | 3,882 | 69 | 7.0x |
| ComplexSelect | quill | 3,097 | 2,968 | 21 | 1.0x |
| | sqlbuilder | 3,068 | 3,866 | 60 | 1.0x |
| | goqu | 10,440 | 11,328 | 329 | 3.4x |
| | squirrel | 12,323 | 10,903 | 180 | 4.0x |
| WideWhere (12 predicates) | quill | 2,770 | 4,920 | 13 | 1.0x |
| | sqlbuilder | 3,555 | 3,794 | 57 | 1.3x |
| | goqu | 7,218 | 7,024 | 214 | 2.6x |
| | squirrel | 15,494 | 12,993 | 230 | 5.6x |
| Insert3Rows | quill | 834 | 968 | 6 | 1.0x |
| | sqlbuilder | 2,393 | 2,169 | 41 | 2.9x |
| | goqu | 3,509 | 3,944 | 105 | 4.2x |
| | squirrel | 5,536 | 4,507 | 93 | 6.6x |
| Update | quill | 738 | 976 | 8 | 1.0x |
| | sqlbuilder | 1,541 | 1,400 | 41 | 2.1x |
| | goqu | 2,470 | 2,616 | 71 | 3.3x |
| | squirrel | 3,954 | 3,218 | 65 | 5.4x |
| Delete | quill | 376 | 720 | 5 | 1.0x |
| | sqlbuilder | 897 | 936 | 28 | 2.4x |
| | goqu | 1,208 | 1,284 | 37 | 3.2x |
| | squirrel | 2,086 | 1,809 | 34 | 5.6x |
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
[GORM](https://gorm.io) and a hand-written `database/sql` loop (`stdlib`) as the floor.

`TestRunScenariosAreComparable` checks every library returns the same result (same id, id sum,
count or rows affected) before any timing counts. Because SQLite time is shared, ratios here are
smaller than in the build-only table; network and disk latency would shrink them further.

### Results

<!-- bench-run:start -->
Apple M1 Pro, Go 1.25.14, median of 6 runs (`make bench-libs`, then `make bench-libs-update`). Lower is
better; the last column is time relative to quill.

| Scenario | Library | ns/op | B/op | allocs/op | Time vs quill |
| --- | --- | ---: | ---: | ---: | ---: |
| SelectOne | quill | 7,658 | 2,232 | 35 | 1.0x |
| | sqlx | 6,355 | 864 | 28 | 0.8x |
| | gorm | 10,481 | 4,905 | 72 | 1.4x |
| | stdlib | 5,694 | 624 | 23 | 0.7x |
| SelectMany | quill | 75,485 | 24,808 | 637 | 1.0x |
| | sqlx | 94,595 | 30,909 | 827 | 1.3x |
| | gorm | 114,861 | 39,725 | 1,271 | 1.5x |
| | stdlib | 72,147 | 12,517 | 714 | 1.0x |
| Count | quill | 61,628 | 1,984 | 27 | 1.0x |
| | sqlx | 59,732 | 480 | 16 | 1.0x |
| | gorm | 63,153 | 3,872 | 47 | 1.0x |
| | stdlib | 59,499 | 400 | 13 | 1.0x |
| Insert | quill | 6,635 | 1,416 | 17 | 1.0x |
| | sqlx | 5,137 | 256 | 9 | 0.8x |
| | gorm | 20,609 | 7,262 | 87 | 3.1x |
| | stdlib | 5,180 | 256 | 9 | 0.8x |
| Update | quill | 4,060 | 976 | 12 | 1.0x |
| | sqlx | 3,241 | 160 | 6 | 0.8x |
| | gorm | 12,055 | 6,821 | 69 | 3.0x |
| | stdlib | 3,242 | 160 | 6 | 0.8x |
<!-- bench-run:end -->


In the run benchmark quill scans many rows as fast as a hand-written `database/sql` loop (SelectMany) and
about 1.3x faster than sqlx, because it reuses its scan targets across rows. Single-row reads are about 1.2x
slower than sqlx and writes 1.2-1.3x slower: the gap is the fixed cost of building the query (clone, clauses,
compiler) that raw SQL strings skip. quill is about 3x faster than GORM on writes. Count is dominated by SQLite
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
