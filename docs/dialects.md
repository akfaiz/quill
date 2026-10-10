# Dialects

You choose the dialect once, with `db.Use(dialect)` or `db.New(conn, dialect)`. The builder API is
the same for all of them; this page lists what differs underneath, and where a feature is missing.

| Dialect | Constant |
| --- | --- |
| PostgreSQL | `db.Postgres` |
| MySQL | `db.MySQL` |
| MariaDB | `db.MariaDB` |
| SQLite | `db.SQLite` |

Tested against PostgreSQL 16, MySQL 8.4, MariaDB 11.4 and SQLite through a pure-Go driver. Other
server versions are untested. SQL Server is not supported. The tables below cover PostgreSQL, MySQL
and SQLite; [MariaDB](#mariadb) is the MySQL column except where its section says otherwise.

## Basics

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| Identifier quoting | `"x"` | `` `x` `` | `"x"` |
| Placeholders | `$1, $2, ...` | `?` | `?` |
| `OFFSET` with no `LIMIT` | `OFFSET n` | adds `LIMIT 18446744073709551615` | adds `LIMIT -1` |
| Random order | `RANDOM()` | `RAND()` / `RAND(seed)` | `RANDOM()` |
| Union members | `(SELECT ...)` | `(SELECT ...)` | `SELECT * FROM (SELECT ...)` |
| Recursive CTE members | `anchor UNION ALL step` | same | same |
| `WithMaterialized` | `as [not] materialized` | `ErrUnsupported` | `ErrUnsupported` |
| Insert, no columns | `DEFAULT VALUES` | `() VALUES ()` | `DEFAULT VALUES` |

## Matching and comparison

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| Case-insensitive `WhereLike` | `ILIKE` | `LIKE` | `LIKE` |
| Case-sensitive `WhereLike` | `LIKE` | `LIKE BINARY` | `GLOB` (`%`→`*`, `_`→`?`, literal `*` `?` escaped) |
| `like` on a column | column cast `::TEXT` | | |
| `WhereNullSafeEquals` | `IS NOT DISTINCT FROM` | `<=>` | `IS` |

## Dates

| Part | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| `WhereDate` | `col::DATE` | `DATE(col)` | `STRFTIME('%Y-%m-%d', col)` |
| `WhereTime` | `col::TIME` | `TIME(col)` | `STRFTIME('%H:%M:%S', col)` |
| `WhereDay` / `Month` / `Year` | `EXTRACT(DAY FROM col)` | `DAY(col)` | `STRFTIME('%d', col)` |

SQLite compares the part as text (`CAST(? AS TEXT)`), which is why numeric day and month values are
zero-padded for every dialect.

## Writes

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| `InsertOrIgnore` | `ON CONFLICT DO NOTHING` | `INSERT IGNORE` | `INSERT OR IGNORE` |
| `Upsert` | `ON CONFLICT (...) DO UPDATE SET x = "excluded"."x"` | `ON DUPLICATE KEY UPDATE x = VALUES(x)` | as PostgreSQL |
| `RETURNING` | yes | `ErrUnsupported` | yes, SQLite 3.35+ |
| Update/delete with a join or limit | `ctid` subquery | native; join plus order/limit is `ErrUnsupported` | `rowid` subquery |
| `UpdateFrom` | yes | `ErrUnsupported` | `ErrUnsupported` |
| `Truncate` | `TRUNCATE ... RESTART IDENTITY` | `TRUNCATE TABLE` | `DELETE` + reset `sqlite_sequence` (one transaction; the reset is skipped when no table uses `AUTOINCREMENT`) |
| `InsertGetID` | `RETURNING "id"` | last insert id | last insert id |
| Rows affected by an update | matched rows | **changed** rows only | matched rows |
| Upsert rows affected | per row | 2 per updated row | per row |

## Joins, locks, hints

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| `JoinLateral`, `LeftJoinLateral` | yes | yes, MySQL 8.0.14+ | `ErrUnsupported` |
| `StraightJoin` | `ErrUnsupported` | yes | `ErrUnsupported` |
| `LockForUpdate` | `FOR UPDATE` | `FOR UPDATE` | ignored |
| `SharedLock` | `FOR SHARE` | `LOCK IN SHARE MODE` | ignored |
| `Lock("...")` | the clause as given | the clause as given | ignored |
| `UseIndex`, `IgnoreIndex` | ignored | `use index (...)`, `ignore index (...)` | ignored |
| `ForceIndex` | ignored | `FORCE INDEX (...)` | `INDEXED BY name` (one name) |

## JSON

The `column->a->b[0]` syntax works everywhere; the SQL differs.

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| Text selector | `"c"->'a'->>'b'` | ``JSON_UNQUOTE(JSON_EXTRACT(`c`, '$."a"."b"'))`` | `JSON_EXTRACT("c", '$."a"."b"')` |
| Compare to a boolean | `("c"->'a')::JSONB = 'true'::JSONB` | `JSON_EXTRACT(...) = TRUE` | `JSON_EXTRACT(...) = TRUE` |
| `WhereJsonContains` | `("c"->'a')::JSONB @> ?` | `JSON_CONTAINS(c, ?, '$."a"')` | `EXISTS (SELECT 1 FROM JSON_EACH(...) WHERE value IS ?)` |
| `WhereJsonContainsKey` | `COALESCE((c)::JSONB ? 'k', FALSE)` | `JSON_CONTAINS_PATH(...)` | `JSON_TYPE(...) IS NOT NULL` |
| `WhereJsonLength` | `JSONB_ARRAY_LENGTH(...)` | `JSON_LENGTH(...)` | `JSON_ARRAY_LENGTH(...)` |
| `WhereJsonOverlaps` | `ErrUnsupported` | `JSON_OVERLAPS(...)` | `ErrUnsupported` |
| JSON path update | `JSONB_SET` | `JSON_SET` | `JSON_SET` |

Differences you can observe:

- **Missing path in `WhereJsonDoesntContain`:** on PostgreSQL and MySQL a missing path makes the
  test `NULL`, so the row is **excluded**; on SQLite it is true.
- **Containment values** are JSON-encoded and bound on PostgreSQL and MySQL; SQLite compares the
  raw value.
- **Updating a nested path** creates only the last key on PostgreSQL and MySQL (the parent must
  exist); SQLite's merge patch creates the parents.
- **PostgreSQL boolean test** needs the `jsonb` cast, which the library adds.

## Behaviour to know about

- **`HAVING` and aliases:** PostgreSQL does not allow output aliases in `HAVING`; use `HavingRaw`
  with the expression.
- **Strict `GROUP BY`:** PostgreSQL and MySQL (`ONLY_FULL_GROUP_BY`) require selected columns to be
  grouped or aggregated. `Count` over a grouped query wraps it, so select the grouped column.
- **Duplicate column names:** `Count` over a grouped, distinct or limited query wraps it in a
  derived table. MySQL rejects duplicate column names in a derived table, so a join that selects
  `*` from two tables sharing a column name should select explicit columns.
- **Affected rows on MySQL** count changed rows, so updating a row to its current value reports 0.
- **Version floors:** SQLite `RETURNING` needs 3.35; MySQL lateral joins need 8.0.14. CTEs need
  MySQL 8.0, MariaDB 10.2 and SQLite 3.8.3; `materialized` needs PostgreSQL 12. The CTE floors
  are the vendors' documented minimums and are untested here.
- **Recursion depth on MySQL** is capped by `cte_max_recursion_depth` (default 1000); a deeper
  recursive CTE fails at run time.

## MariaDB

`db.MariaDB` uses MySQL's grammar (backtick quoting, `?` placeholders, `INSERT IGNORE`,
`ON DUPLICATE KEY UPDATE`, native `UPDATE ... JOIN`, `DELETE alias FROM`, `STRAIGHT_JOIN`, index
hints, `LOCK IN SHARE MODE`, `TRUNCATE TABLE`). The generated SQL is **identical to MySQL's**
except in two places, both of which the server rejects otherwise:

| | MySQL | MariaDB |
| --- | --- | --- |
| `JoinLateral`, `LeftJoinLateral` | `lateral` (8.0.14+) | `db.ErrUnsupported`: MariaDB has no `LATERAL` |
| JSON container value in an update (a map or slice) | ``CAST(? AS JSON)`` | ``JSON_QUERY(?, '$')``: MariaDB has no `CAST AS JSON` |

Verified on MariaDB 11.4 against a live server:

- **JSON text selectors are not changed.** `col->path` still renders as
  ``JSON_UNQUOTE(JSON_EXTRACT(col, path))``, which returns arrays and objects as JSON text exactly
  as the other dialects do. MariaDB's `json_value` would return `NULL` for arrays and objects (and
  `1` for a JSON `true`), so it is deliberately not used.
- **`WhereJsonDoesntContain` on a missing path includes the row** (like SQLite): MariaDB's
  `json_contains` returns a definite false there, where MySQL returns `NULL` and excludes it.
- `json_overlaps` and `json_contains_path` work, so `WhereJsonOverlaps` is available.
- **`RETURNING`:** the server supports it for `INSERT` and `DELETE` but not `UPDATE`. quill does not
  use it yet, so `Returning(...)` returns `db.ErrUnsupported` on MariaDB as on MySQL.
- Affected-row counts follow MySQL (an update reports changed rows).

## Adding another dialect

A dialect is a `Grammar` implementation in `internal/dialect` plus a `Dialect` constant. See
[Contributing](../CONTRIBUTING.md#adding-a-dialect).
