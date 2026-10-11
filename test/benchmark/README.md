# Library comparison benchmarks

Compares how fast quill builds SQL against three other Go query builders:
[go-sqlbuilder](https://github.com/huandu/go-sqlbuilder) v1.44.0,
[goqu](https://github.com/doug-martin/goqu) v9.19.0 and
[squirrel](https://github.com/Masterminds/squirrel) v1.5.4. This is a separate module, so the
root module stays free of third-party dependencies.

```sh
make bench-libs                 # from the repo root: 6 runs per benchmark, saved to results.txt
make bench-libs-update          # rewrite the Results table below from results.txt (no re-run)
cd test/benchmark && go test -v -run Comparable .   # print the SQL each library produces
```

## What is measured

Only **building** a query: from the first builder call to the `(sql, args)` pair. No
database, no network, no execution. Every scenario targets PostgreSQL with bound `$n`
placeholders, which is what an application actually sends.

`TestScenariosAreComparable` runs first in spirit: every library must build each query
without error and bind the same number of arguments, so a benchmark cannot win by quietly
building a different query. goqu and sqlbuilder bind `LIMIT` and `OFFSET` as parameters
(quill and squirrel inline them as literals), and the test accounts for that explicitly.

## Results

<!-- bench:start -->
Apple M1 Pro, Go 1.25.14, median of 6 runs (`make bench-libs`, then `make bench-libs-update`). Lower is
better; the last column is time relative to quill.

| Scenario | Library | ns/op | B/op | allocs/op | Time vs quill |
| --- | --- | ---: | ---: | ---: | ---: |
| SimpleSelect | quill | 550 | 864 | 8 | 1.0x |
| | sqlbuilder | 1,035 | 1,488 | 37 | 1.9x |
| | goqu | 2,231 | 3,832 | 77 | 4.1x |
| | squirrel | 3,778 | 3,881 | 69 | 6.9x |
| ComplexSelect | quill | 2,591 | 2,968 | 20 | 1.0x |
| | sqlbuilder | 2,413 | 3,865 | 60 | 0.9x |
| | goqu | 8,484 | 11,328 | 329 | 3.3x |
| | squirrel | 9,993 | 10,900 | 180 | 3.9x |
| WideWhere (12 predicates) | quill | 2,017 | 4,920 | 13 | 1.0x |
| | sqlbuilder | 2,719 | 3,793 | 57 | 1.3x |
| | goqu | 5,990 | 7,024 | 214 | 3.0x |
| | squirrel | 12,762 | 12,989 | 230 | 6.3x |
| Insert3Rows | quill | 893 | 1,072 | 14 | 1.0x |
| | sqlbuilder | 1,907 | 2,168 | 41 | 2.1x |
| | goqu | 2,881 | 3,944 | 105 | 3.2x |
| | squirrel | 4,411 | 4,505 | 93 | 4.9x |
| Update | quill | 821 | 1,040 | 16 | 1.0x |
| | sqlbuilder | 1,247 | 1,400 | 41 | 1.5x |
| | goqu | 1,973 | 2,616 | 71 | 2.4x |
| | squirrel | 3,178 | 3,217 | 65 | 3.9x |
| Delete | quill | 354 | 720 | 7 | 1.0x |
| | sqlbuilder | 695 | 936 | 28 | 2.0x |
| | goqu | 990 | 1,284 | 37 | 2.8x |
| | squirrel | 1,685 | 1,808 | 34 | 4.8x |
<!-- bench:end -->

sqlbuilder is the closest competitor. On ComplexSelect it is level with quill (about 1.0x;
run-to-run noise is around 5-10%), even though quill makes about a third as many allocations.
On WideWhere and Insert3Rows quill allocates more bytes than it does.

Absolute numbers depend on the machine; compare the ratios, and re-run on your own
hardware before quoting them.

## SQL each library generates

The real output of `go test -v -run Comparable .`, pasted here. It is a snapshot: re-run that
command and update this section if a library version or a scenario changes. Arguments are
what is bound to the `$n` placeholders.

### SimpleSelect

```sql
-- quill   args: [100]
SELECT "id", "name" FROM "users" WHERE "votes" > $1 ORDER BY "id" DESC LIMIT 10

-- sqlbuilder   args: [100 10]
SELECT id, name FROM users WHERE votes > $1 ORDER BY id DESC LIMIT $2

-- goqu   args: [100 10]
SELECT "id", "name" FROM "users" WHERE ("votes" > $1) ORDER BY "id" DESC LIMIT $2

-- squirrel   args: [100]
SELECT id, name FROM users WHERE votes > $1 ORDER BY id DESC LIMIT 10
```

### ComplexSelect

```sql
-- quill   args: [18 1 2 3 4 5 1]
SELECT "u"."id", "u"."name", "p"."title" FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" LEFT JOIN "profiles" ON "profiles"."user_id" = "u"."id" WHERE "u"."age" >= $1 AND "u"."id" IN ($2, $3, $4, $5, $6) AND "u"."deleted_at" IS NULL GROUP BY "u"."id", "u"."name", "p"."title" HAVING "count(*)" > $7 ORDER BY "u"."name" ASC LIMIT 50 OFFSET 100

-- sqlbuilder   args: [18 1 2 3 4 5 1 50 100]
SELECT u.id, u.name, p.title FROM users AS u JOIN posts AS p ON p.user_id = u.id LEFT JOIN profiles ON profiles.user_id = u.id WHERE u.age >= $1 AND u.id IN ($2, $3, $4, $5, $6) AND u.deleted_at IS NULL GROUP BY u.id, u.name, p.title HAVING count(*) > $7 ORDER BY u.name ASC LIMIT $8 OFFSET $9

-- goqu   args: [18 1 2 3 4 5 1 50 100]
SELECT "u"."id", "u"."name", "p"."title" FROM "users" AS "u" INNER JOIN "posts" AS "p" ON ("p"."user_id" = "u"."id") LEFT JOIN "profiles" ON ("profiles"."user_id" = "u"."id") WHERE (("u"."age" >= $1) AND ("u"."id" IN ($2, $3, $4, $5, $6)) AND ("u"."deleted_at" IS NULL)) GROUP BY "u"."id", "u"."name", "p"."title" HAVING (COUNT(*) > $7) ORDER BY "u"."name" ASC LIMIT $8 OFFSET $9

-- squirrel   args: [18 1 2 3 4 5 1]
SELECT u.id, u.name, p.title FROM users AS u JOIN posts AS p ON p.user_id = u.id LEFT JOIN profiles ON profiles.user_id = u.id WHERE u.age >= $1 AND u.id IN ($2,$3,$4,$5,$6) AND u.deleted_at IS NULL GROUP BY u.id, u.name, p.title HAVING count(*) > $7 ORDER BY u.name ASC LIMIT 50 OFFSET 100
```

### WideWhere (12 predicates)

```sql
-- quill   args: [1 2 3 4 5 6 1 2 3 4 5 7 8 x% 9]
SELECT * FROM "orders" WHERE "a" = $1 AND "b" > $2 AND "c" < $3 AND "d" != $4 AND "e" >= $5 AND "f" <= $6 AND "g" IN ($7, $8, $9, $10, $11) AND "h" IS NULL AND "i" IS NOT NULL AND "j" BETWEEN $12 AND $13 AND "k"::TEXT ILIKE $14 AND "l" = $15

-- sqlbuilder   args: [1 2 3 4 5 6 1 2 3 4 5 7 8 x% 9]
SELECT * FROM orders WHERE a = $1 AND b > $2 AND c < $3 AND d <> $4 AND e >= $5 AND f <= $6 AND g IN ($7, $8, $9, $10, $11) AND h IS NULL AND i IS NOT NULL AND j BETWEEN $12 AND $13 AND k LIKE $14 AND l = $15

-- goqu   args: [1 2 3 4 5 6 1 2 3 4 5 7 8 x% 9]
SELECT * FROM "orders" WHERE (("a" = $1) AND ("b" > $2) AND ("c" < $3) AND ("d" != $4) AND ("e" >= $5) AND ("f" <= $6) AND ("g" IN ($7, $8, $9, $10, $11)) AND ("h" IS NULL) AND ("i" IS NOT NULL) AND ("j" BETWEEN $12 AND $13) AND ("k" LIKE $14) AND ("l" = $15))

-- squirrel   args: [1 2 3 4 5 6 1 2 3 4 5 7 8 x% 9]
SELECT * FROM orders WHERE a = $1 AND b > $2 AND c < $3 AND d <> $4 AND e >= $5 AND f <= $6 AND g IN ($7,$8,$9,$10,$11) AND h IS NULL AND i IS NOT NULL AND j BETWEEN $12 AND $13 AND k LIKE $14 AND l = $15
```

### Insert3Rows

```sql
-- quill   args: [30 a@x ann 25 b@x bob 41 c@x cy]
INSERT INTO "users" ("age", "email", "name") VALUES ($1, $2, $3), ($4, $5, $6), ($7, $8, $9)

-- sqlbuilder   args: [ann 30 a@x bob 25 b@x cy 41 c@x]
INSERT INTO users (name, age, email) VALUES ($1, $2, $3), ($4, $5, $6), ($7, $8, $9)

-- goqu   args: [ann 30 a@x bob 25 b@x cy 41 c@x]
INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3), ($4, $5, $6), ($7, $8, $9)

-- squirrel   args: [ann 30 a@x bob 25 b@x cy 41 c@x]
INSERT INTO users (name,age,email) VALUES ($1,$2,$3),($4,$5,$6),($7,$8,$9)
```

### Update

```sql
-- quill   args: [31 ann 1]
UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3

-- sqlbuilder   args: [ann 31 1]
UPDATE users SET name = $1, age = $2 WHERE id = $3

-- goqu   args: [31 ann 1]
UPDATE "users" SET "age"=$1,"name"=$2 WHERE ("id" = $3)

-- squirrel   args: [ann 31 1]
UPDATE users SET name = $1, age = $2 WHERE id = $3
```

### Delete

```sql
-- quill   args: [1]
DELETE FROM "users" WHERE "id" = $1

-- sqlbuilder   args: [1]
DELETE FROM users WHERE id = $1

-- goqu   args: [1]
DELETE FROM "users" WHERE ("id" = $1)

-- squirrel   args: [1]
DELETE FROM users WHERE id = $1
```

## Reading the numbers fairly

- **Build time is rarely the bottleneck.** A query round trip to a database costs
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
- **Not covered:** result scanning, struct mapping, CTEs/unions/JSON (not every library
  supports them), and one-time setup cost such as `goqu.Dialect(...)`.
- Library features differ. This compares speed on queries all four can express, not
  which library is better for your use case.
