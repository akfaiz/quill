# Security

This page lists what the library protects you from, what it cannot, and what to do about the
rest. Each section names the risk area.

- [Injection](#injection) · [Raw SQL](#raw-sql) · [Dynamic identifiers](#dynamic-identifiers)
- [Mass writes](#mass-writes) · [Logging and personal data](#logging-and-personal-data)
- [Cursors](#cursors) · [Resource handling](#resource-handling) · [Checklist](#checklist)

## Injection

*Risk area: input validation.*

What the library does for you:

| Input | Protection |
| --- | --- |
| Values (`Where`, `Insert`, `Update`, ...) | Always sent as bind arguments, never put in the SQL text |
| Identifiers (tables, columns, aliases) | Always quoted; an embedded quote character is doubled, so a name cannot close its own quotes |
| Operators | Allow-listed; anything else is `db.ErrInvalidOperator` |
| Order direction | Only `asc` or `desc`; anything else is `db.ErrInvalidDirection` |
| JSON paths | Single quotes are escaped (backslashes before them dropped), so a path stays inside its SQL literal |
| Index names | Validated against `[a-zA-Z0-9_$]` because they are emitted unquoted |
| `WhereIntegerInRaw` | Only integers are accepted; anything else is `db.ErrInvalidArgs` |

Quoting makes an identifier *safe to embed*, not *safe to choose*: see
[dynamic identifiers](#dynamic-identifiers).

## Raw SQL

*Risk area: injection through trusted-text APIs.*

These methods insert SQL text exactly as written. **Never build their text from user input.**

`db.Raw`, `SelectRaw`, `WhereRaw`, `OrWhereRaw`, `HavingRaw`, `OrHavingRaw`, `GroupByRaw`,
`OrderByRaw`, `FromRaw`, `Lock`, the join clause's `WhereRaw` / `OrWhereRaw`, and `DB.Query` /
`DB.Exec`.

```go
q := conn.Table("users")

// wrong: user input becomes SQL
q.WhereRaw("name = '" + name + "'")

// right: the value is an argument
q.WhereRaw("lower(name) = ?", strings.ToLower(name))
q.Where("name", name)
```

`?` in raw text becomes a bind marker (`$n` on PostgreSQL). Markers inside quoted strings or
quoted identifiers are left alone, and `??` is a literal `?`.

## Dynamic identifiers

*Risk area: access control.*

If a column, table or sort field comes from a request (`?sort=created_at`), quoting prevents SQL
injection but a caller could still pick a column you did not mean to expose, such as
`password_hash`. Allow-list the names:

```go
q := conn.Table("users")
var sortable = map[string]string{"created": "created_at", "name": "name"}

col, ok := sortable[r.URL.Query().Get("sort")]
if !ok { col = "created_at" }
q.OrderBy(col, dir)       // dir is validated to asc/desc by the library
```

The same goes for column names passed to `Where`, `Select`, `Pluck` and the cursor column.

## Mass writes

*Risk area: data integrity.*

An unbounded `UPDATE` or `DELETE` is refused with `db.ErrNoWhere` unless the query has a `WHERE`
or `LIMIT`, or you call `AllRows()` to say you mean every row. `Truncate` is the explicit way to
empty a table, and on PostgreSQL it does not cascade to referencing tables.
[Details](writing-data.md#the-where-guard).

`UpdateOrInsert` is not atomic. If concurrent writers could create duplicates, use a transaction
and a unique index (or `Upsert`).

## Logging and personal data

*Risk area: data exposure.*

Bind values routinely hold personal data (names, emails, identity numbers, card or account
numbers).

- `db.WithHook` events carry the SQL (with placeholders) and the argument **count**, not the
  values.
- `db.WithLogArgs()` adds the values to `QueryEvent.Args`. Enable it only where that data may be
  logged, stored and access-controlled accordingly.
- `ToRawSQL()` and `MustRawSQL()` interpolate values into the text. They exist for debugging only:
  **never execute or log their output in production**, because it is not injection-safe and it
  contains the data.
- Driver and database errors can echo values back; treat `QueryEvent.Err` as sensitive too.

## Cursors

*Risk area: untrusted input.*

A keyset cursor (`CursorPaginate`) is a token that travels through clients, so it is parsed as
untrusted input. It decodes to a plain string, boolean or number that is **only bound as an
argument**; a forged token can move the page position or be rejected with `db.ErrInvalidArgs`, but
it cannot alter the SQL. The cursor column comes from your code, never from the token.

The token is not encrypted or signed. It reveals the last row's key value, so do not use it for
anything secret. If positions must not be forgeable, sign the token in your own layer.

## Resource handling

*Risk area: availability.*

- Rows are always closed, including when your callback returns an error.
- Every method honours the `context`, so a cancelled request stops its queries.
- `Stream` and `Each` hold one row in memory, and breaking out of a `Stream` loop closes the cursor.
- Transactions roll back on an error and on a panic, then re-raise the panic.
- `TableName()` is run behind a `recover`, so a faulty method returns an error instead of
  crashing the process.

## Checklist

- Values go in as arguments, never into raw text.
- Names that come from input are allow-listed.
- `*Raw` methods only ever receive literals you wrote.
- Hooks do not log arguments unless that is intended and controlled.
- `ToRawSQL` is not used outside local debugging.
- Cursor columns and sort fields are fixed by your code.
- Multi-statement changes that must stay consistent run in a `Transaction`.
