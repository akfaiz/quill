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
| SimpleSelect | quill | 681 | 864 | 8 | 1.0x |
| | sqlbuilder | 1,376 | 1,488 | 37 | 2.0x |
| | goqu | 2,973 | 3,832 | 77 | 4.4x |
| | squirrel | 4,646 | 3,882 | 69 | 6.8x |
| ComplexSelect | quill | 3,090 | 2,968 | 20 | 1.0x |
| | sqlbuilder | 3,176 | 3,866 | 60 | 1.0x |
| | goqu | 10,653 | 11,328 | 329 | 3.4x |
| | squirrel | 12,543 | 10,904 | 180 | 4.1x |
| WideWhere (12 predicates) | quill | 2,631 | 4,920 | 13 | 1.0x |
| | sqlbuilder | 3,585 | 3,794 | 57 | 1.4x |
| | goqu | 7,235 | 7,024 | 214 | 2.7x |
| | squirrel | 15,625 | 12,993 | 230 | 5.9x |
| Insert3Rows | quill | 1,014 | 1,072 | 14 | 1.0x |
| | sqlbuilder | 2,373 | 2,169 | 41 | 2.3x |
| | goqu | 3,479 | 3,944 | 105 | 3.4x |
| | squirrel | 5,578 | 4,507 | 93 | 5.5x |
| Update | quill | 993 | 1,040 | 16 | 1.0x |
| | sqlbuilder | 1,587 | 1,400 | 41 | 1.6x |
| | goqu | 2,556 | 2,616 | 71 | 2.6x |
| | squirrel | 4,077 | 3,218 | 65 | 4.1x |
| Delete | quill | 430 | 720 | 7 | 1.0x |
| | sqlbuilder | 883 | 936 | 28 | 2.1x |
| | goqu | 1,192 | 1,284 | 37 | 2.8x |
| | squirrel | 2,070 | 1,809 | 34 | 4.8x |
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
| SelectOne | quill | 9,193 | 3,576 | 52 | 1.0x |
| | sqlx | 6,220 | 864 | 28 | 0.7x |
| | gorm | 10,742 | 4,905 | 72 | 1.2x |
| | stdlib | 5,794 | 624 | 23 | 0.6x |
| SelectMany | quill | 147,237 | 71,601 | 2,135 | 1.0x |
| | sqlx | 93,649 | 30,910 | 827 | 0.6x |
| | gorm | 116,121 | 39,727 | 1,271 | 0.8x |
| | stdlib | 73,289 | 12,517 | 714 | 0.5x |
| Count | quill | 62,520 | 1,872 | 27 | 1.0x |
| | sqlx | 60,241 | 480 | 16 | 1.0x |
| | gorm | 64,421 | 3,872 | 47 | 1.0x |
| | stdlib | 60,329 | 400 | 13 | 1.0x |
| Insert | quill | 7,014 | 1,104 | 22 | 1.0x |
| | sqlx | 5,282 | 256 | 9 | 0.8x |
| | gorm | 20,700 | 7,261 | 87 | 3.0x |
| | stdlib | 5,277 | 256 | 9 | 0.8x |
| Update | quill | 4,442 | 1,016 | 18 | 1.0x |
| | sqlx | 3,297 | 160 | 6 | 0.7x |
| | gorm | 12,449 | 6,820 | 69 | 2.8x |
| | stdlib | 3,281 | 160 | 6 | 0.7x |
<!-- bench-run:end -->


In the run benchmark quill is 1.4-1.6x slower than sqlx on selects and 1.3-1.4x on writes, and well ahead
of GORM on writes (about 3x). It allocates more than sqlx on selects (struct mapping through `internal/rowmap`),
so there is room to improve there. Count is dominated by SQLite itself, so every library ties.

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
