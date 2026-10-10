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
| `OFFSET` with no `LIMIT` | `offset n` | adds `limit 18446744073709551615` | adds `limit -1` |
| Random order | `RANDOM()` | `RAND()` / `RAND(seed)` | `RANDOM()` |
| Union members | `(select ...)` | `(select ...)` | `select * from (select ...)` |
| Insert, no columns | `default values` | `() values ()` | `default values` |

## Matching and comparison

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| Case-insensitive `WhereLike` | `ilike` | `like` | `like` |
| Case-sensitive `WhereLike` | `like` | `like binary` | `glob` (`%`→`*`, `_`→`?`, literal `*` `?` escaped) |
| `like` on a column | column cast `::text` | | |
| `WhereNullSafeEquals` | `is not distinct from` | `<=>` | `is` |

## Dates

| Part | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| `WhereDate` | `col::date` | `date(col)` | `strftime('%Y-%m-%d', col)` |
| `WhereTime` | `col::time` | `time(col)` | `strftime('%H:%M:%S', col)` |
| `WhereDay` / `Month` / `Year` | `extract(day from col)` | `day(col)` | `strftime('%d', col)` |

SQLite compares the part as text (`cast(? as text)`), which is why numeric day and month values are
zero-padded for every dialect.

## Writes

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| `InsertOrIgnore` | `on conflict do nothing` | `insert ignore` | `insert or ignore` |
| `Upsert` | `on conflict (...) do update set x = "excluded"."x"` | `on duplicate key update x = values(x)` | as PostgreSQL |
| `RETURNING` | yes | `ErrUnsupported` | yes, SQLite 3.35+ |
| Update/delete with a join or limit | `ctid` subquery | native | `rowid` subquery |
| `UpdateFrom` | yes | `ErrUnsupported` | `ErrUnsupported` |
| `Truncate` | `truncate ... restart identity` | `truncate table` | `delete` + reset `sqlite_sequence` |
| `InsertGetID` | `returning "id"` | last insert id | last insert id |
| Rows affected by an update | matched rows | **changed** rows only | matched rows |
| Upsert rows affected | per row | 2 per updated row | per row |

## Joins, locks, hints

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| `JoinLateral`, `LeftJoinLateral` | yes | yes, MySQL 8.0.14+ | `ErrUnsupported` |
| `StraightJoin` | `ErrUnsupported` | yes | `ErrUnsupported` |
| `LockForUpdate` | `for update` | `for update` | ignored |
| `SharedLock` | `for share` | `lock in share mode` | ignored |
| `Lock("...")` | the clause as given | the clause as given | ignored |
| `UseIndex`, `IgnoreIndex` | ignored | `use index (...)`, `ignore index (...)` | ignored |
| `ForceIndex` | ignored | `force index (...)` | `indexed by name` (one name) |

## JSON

The `column->a->b[0]` syntax works everywhere; the SQL differs.

| | PostgreSQL | MySQL | SQLite |
| --- | --- | --- | --- |
| Text selector | `"c"->'a'->>'b'` | ``json_unquote(json_extract(`c`, '$."a"."b"'))`` | `json_extract("c", '$."a"."b"')` |
| Compare to a boolean | `("c"->'a')::jsonb = 'true'::jsonb` | `json_extract(...) = true` | `json_extract(...) = true` |
| `WhereJsonContains` | `("c"->'a')::jsonb @> ?` | `json_contains(c, ?, '$."a"')` | `exists (select 1 from json_each(...) where value is ?)` |
| `WhereJsonContainsKey` | `coalesce((c)::jsonb ? 'k', false)` | `json_contains_path(...)` | `json_type(...) is not null` |
| `WhereJsonLength` | `jsonb_array_length(...)` | `json_length(...)` | `json_array_length(...)` |
| `WhereJsonOverlaps` | `ErrUnsupported` | `json_overlaps(...)` | `ErrUnsupported` |
| JSON path update | `jsonb_set` | `json_set` | `json_patch` |

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
- **Version floors:** SQLite `RETURNING` needs 3.35; MySQL lateral joins need 8.0.14.

## MariaDB

`db.MariaDB` uses MySQL's grammar (backtick quoting, `?` placeholders, `insert ignore`,
`on duplicate key update`, native `update ... join`, `delete alias from`, `straight_join`, index
hints, `lock in share mode`, `truncate table`). The generated SQL is **identical to MySQL's**
except in two places, both of which the server rejects otherwise:

| | MySQL | MariaDB |
| --- | --- | --- |
| `JoinLateral`, `LeftJoinLateral` | `lateral` (8.0.14+) | `db.ErrUnsupported`: MariaDB has no `LATERAL` |
| JSON container value in an update (a map or slice) | ``cast(? as json)`` | ``json_query(?, '$')``: MariaDB has no `CAST AS JSON` |

Verified on MariaDB 11.4 against a live server:

- **JSON text selectors are not changed.** `col->path` still renders as
  ``json_unquote(json_extract(col, path))``, which returns arrays and objects as JSON text exactly
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
