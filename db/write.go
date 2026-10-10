package db

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/akfaiz/quill/internal/jsonpath"
)

// Stmt is one compiled statement and its ordered bind arguments.
type Stmt struct {
	SQL  string
	Args []any
}

type setItem struct {
	column string
	value  any
}

var tableAliasRe = regexp.MustCompile(`(?i)\s+as\s+`)

// Returning adds a RETURNING clause to insert/update/delete/upsert
// (Postgres, SQLite >= 3.35; MySQL returns ErrUnsupported). "*" is allowed.
//
// Example:
//
//	conn.Table("users").Returning("id", "created_at").InsertSQL(map[string]any{"name": "Ann"})
//	// INSERT INTO "users" ("name") VALUES ($1) RETURNING "id", "created_at"   args: [Ann]
//	// MySQL: InsertSQL returns ErrUnsupported
func (b *Builder) Returning(columns ...string) *Builder {
	b.returning = append(b.returning, columns...)
	return b
}

// AllRows confirms that an update/delete without a where clause is intended.
// Without it, UpdateSQL/DeleteSQL/IncrementSQL refuse to build (ErrNoWhere).
//
// Example:
//
//	conn.Table("sessions").AllRows().DeleteSQL()
//	// DELETE FROM "sessions"   args: []
func (b *Builder) AllRows() *Builder { b.allRows = true; return b }

func (b *Builder) writeCompiler() (*compiler, error) {
	c, err := b.newCompiler()
	if err != nil {
		return nil, err
	}
	if b.table == "" {
		return nil, ErrNoTable
	}
	if len(b.ctes) > 0 {
		// put the WITH on the select passed to InsertUsing instead
		return nil, fmt.Errorf("%w: with clause on a write statement", ErrUnsupported)
	}
	return c, nil
}

func (b *Builder) requireWhere() error {
	if len(b.wheres) == 0 && b.limit == nil && !b.allRows {
		return ErrNoWhere
	}
	return nil
}

func (c *compiler) result(err error) (string, []any, error) {
	if err != nil {
		return "", nil, err
	}
	return c.sb.String(), c.args, nil
}

// ---- insert ----

// normalizeRows returns the sorted column list shared by every row.
func normalizeRows(rows []map[string]any) ([]string, error) {
	if len(rows) == 0 {
		return nil, ErrNoValues
	}
	cols := make([]string, 0, len(rows[0]))
	for k := range rows[0] {
		cols = append(cols, k)
	}
	sort.Strings(cols)
	for i, r := range rows[1:] {
		if len(r) != len(cols) {
			return nil, fmt.Errorf("%w: row %d has different columns than row 0", ErrInvalidArgs, i+1)
		}
		for _, k := range cols {
			if _, ok := r[k]; !ok {
				return nil, fmt.Errorf("%w: row %d is missing column %q", ErrInvalidArgs, i+1, k)
			}
		}
	}
	if len(cols) == 0 && len(rows) > 1 {
		return nil, fmt.Errorf("%w: multiple rows need at least one column", ErrInvalidArgs)
	}
	return cols, nil
}

func (c *compiler) insertVerb(ignore bool) string {
	if ignore {
		switch c.d {
		case MySQL, MariaDB:
			return "INSERT IGNORE INTO "
		case SQLite:
			return "INSERT OR IGNORE INTO "
		}
	}
	return "INSERT INTO "
}

func (c *compiler) wrapColumns(cols []string) string {
	out := make([]string, len(cols))
	for i, col := range cols {
		out[i] = c.wrapColumn(col)
	}
	return strings.Join(out, ", ")
}

func (c *compiler) returningClause(cols []string) error {
	if len(cols) == 0 {
		return nil
	}
	if c.d.mysqlFamily() {
		return fmt.Errorf("%w: RETURNING on mysql", ErrUnsupported)
	}
	c.write(" RETURNING " + c.wrapColumns(cols))
	return nil
}

func (c *compiler) insert(b *Builder, rows []map[string]any, ignore bool) error {
	cols, err := normalizeRows(rows)
	if err != nil {
		return err
	}
	c.write(c.insertVerb(ignore) + c.wrapTable(b.table))
	if len(cols) == 0 {
		if c.d.mysqlFamily() {
			c.write(" () VALUES ()")
		} else {
			c.write(" DEFAULT VALUES")
		}
	} else {
		c.write(" (" + c.wrapColumns(cols) + ") VALUES ")
		for i, r := range rows {
			if i > 0 {
				c.write(", ")
			}
			c.write("(")
			for j, k := range cols {
				if j > 0 {
					c.write(", ")
				}
				if err := c.value(r[k]); err != nil {
					return err
				}
			}
			c.write(")")
		}
	}
	if ignore && c.d == Postgres {
		c.write(" ON CONFLICT DO NOTHING")
	}
	return c.returningClause(b.returning)
}

// InsertSQL builds "INSERT INTO t (cols) VALUES (...), (...)". Columns are the
// sorted keys of the first row; every row must have the same keys. A single
// empty row inserts default values.
//
// Example:
//
//	conn.Table("users").InsertSQL(map[string]any{"name": "Ann", "email": "ann@example.com"})
//	// INSERT INTO "users" ("email", "name") VALUES ($1, $2)   args: [ann@example.com Ann]
func (b *Builder) InsertSQL(rows ...map[string]any) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insert(b, rows, false))
}

// InsertOrIgnoreSQL builds an insert that skips conflicting rows
// (mysql "INSERT IGNORE", sqlite "INSERT OR IGNORE", postgres "ON CONFLICT DO NOTHING").
//
// Example:
//
//	conn.Table("users").InsertOrIgnoreSQL(map[string]any{"email": "ann@example.com"})
//	// INSERT INTO "users" ("email") VALUES ($1) ON CONFLICT DO NOTHING
//	// MySQL:  INSERT IGNORE INTO `users` (`email`) VALUES (?)
//	// SQLite: INSERT OR IGNORE INTO "users" ("email") VALUES (?)
func (b *Builder) InsertOrIgnoreSQL(rows ...map[string]any) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insert(b, rows, true))
}

// InsertGetIDSQL builds a single-row insert. On Postgres it appends
// `RETURNING "<sequence>"` (default "id"); on MySQL/SQLite read the id from
// the driver's LastInsertId.
//
// Example:
//
//	conn.Table("users").InsertGetIDSQL(map[string]any{"name": "Ann"})
//	// INSERT INTO "users" ("name") VALUES ($1) RETURNING "id"   args: [Ann]
//	// MySQL: INSERT INTO `users` (`name`) VALUES (?)   (no RETURNING)
func (b *Builder) InsertGetIDSQL(row map[string]any, sequence ...string) (string, []any, error) {
	if b.dialect == Postgres {
		seq := "id"
		if len(sequence) > 0 && sequence[0] != "" {
			seq = sequence[0]
		}
		b = b.Clone()
		b.returning = []string{seq}
	}
	return b.InsertSQL(row)
}

func (c *compiler) insertUsing(b *Builder, columns []string, sub *Builder, ignore bool) error {
	if sub == nil {
		return fmt.Errorf("%w: nil subquery", ErrInvalidArgs)
	}
	if sub.err != nil {
		return sub.err
	}
	c.write(c.insertVerb(ignore) + c.wrapTable(b.table))
	if len(columns) > 0 && (len(columns) != 1 || columns[0] != "*") {
		c.write(" (" + c.wrapColumns(columns) + ")")
	}
	c.write(" ")
	if err := c.selectQuery(sub); err != nil {
		return err
	}
	if ignore && c.d == Postgres {
		c.write(" ON CONFLICT DO NOTHING")
	}
	return c.returningClause(b.returning)
}

// InsertUsingSQL builds "INSERT INTO t (cols) SELECT ...".
//
// Example:
//
//	sub := conn.Table("users").Select("name", "email").Where("active", true)
//	conn.Table("archived_users").InsertUsingSQL([]string{"name", "email"}, sub)
//	// INSERT INTO "archived_users" ("name", "email")
//	//   SELECT "name", "email" FROM "users" WHERE "active" = $1   args: [true]
func (b *Builder) InsertUsingSQL(columns []string, sub *Builder) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insertUsing(b, columns, sub, false))
}

// InsertOrIgnoreUsingSQL is InsertUsingSQL that skips conflicting rows.
//
// Example:
//
//	sub := conn.Table("users").Select("name", "email")
//	conn.Table("archived_users").InsertOrIgnoreUsingSQL([]string{"name", "email"}, sub)
//	// INSERT INTO "archived_users" ("name", "email")
//	//   SELECT "name", "email" FROM "users" ON CONFLICT DO NOTHING   args: []
func (b *Builder) InsertOrIgnoreUsingSQL(columns []string, sub *Builder) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insertUsing(b, columns, sub, true))
}

// ---- upsert ----

// UpsertSQL builds an insert that updates on conflict. update lists the columns
// that take the inserted value (nil means every inserted column). uniqueBy is
// required on Postgres/SQLite (conflict target) and ignored on MySQL.
//
// Example:
//
//	rows := []map[string]any{{"email": "ann@example.com", "name": "Ann"}}
//	conn.Table("users").UpsertSQL(rows, []string{"email"}, []string{"name"})
//	// MySQL: INSERT INTO `users` (`email`, `name`) VALUES (?, ?)
//	//        ON DUPLICATE KEY UPDATE `name` = VALUES(`name`)   args: [ann@example.com Ann]
func (b *Builder) UpsertSQL(rows []map[string]any, uniqueBy, update []string) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	cols, err := normalizeRows(rows)
	if err != nil {
		return "", nil, err
	}
	if len(cols) == 0 {
		return "", nil, fmt.Errorf("%w: upsert needs at least one column", ErrInvalidArgs)
	}
	if update == nil {
		update = cols
	}
	if len(update) == 0 {
		return "", nil, fmt.Errorf("%w: upsert has no columns to update", ErrInvalidArgs)
	}
	if !c.d.mysqlFamily() && len(uniqueBy) == 0 {
		return "", nil, fmt.Errorf("%w: upsert needs uniqueBy columns on %s", ErrInvalidArgs, c.d)
	}
	if err := c.insert(b.withoutReturning(), rows, false); err != nil {
		return "", nil, err
	}
	sets := make([]string, len(update))
	if c.d.mysqlFamily() {
		c.write(" ON DUPLICATE KEY UPDATE ")
		for i, u := range update {
			sets[i] = c.wrapColumn(u) + " = VALUES(" + c.wrapColumn(u) + ")"
		}
	} else {
		c.write(" ON CONFLICT (" + c.wrapColumns(uniqueBy) + ") DO UPDATE SET ")
		for i, u := range update {
			sets[i] = c.wrapColumn(u) + " = " + c.g.Quote("excluded") + "." + c.wrapColumn(u)
		}
	}
	c.write(strings.Join(sets, ", "))
	return c.result(c.returningClause(b.returning))
}

func (b *Builder) withoutReturning() *Builder {
	n := *b
	n.returning = nil
	return &n
}

// ---- update ----

func sortedSets(values map[string]any) []setItem {
	items := make([]setItem, 0, len(values))
	for k, v := range values {
		items = append(items, setItem{k, v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].column < items[j].column })
	return items
}

// setColumn wraps an update target. Postgres/SQLite reject qualified SET
// targets, so only the last segment is kept; MySQL keeps the qualifier
// (needed for multi-table updates).
func (c *compiler) setColumn(key string) string {
	if !c.d.mysqlFamily() {
		if i := strings.LastIndex(key, "."); i >= 0 {
			key = key[i+1:]
		}
	}
	return c.wrapColumn(key)
}

func (c *compiler) setList(sets []setItem) error {
	written := false
	done := map[string]bool{}
	for _, s := range sets {
		if jsonpath.IsSelector(s.column) {
			key := c.jsonColumnKey(s.column)
			if done[key] {
				continue
			}
			done[key] = true
			var group []setItem
			for _, o := range sets {
				if jsonpath.IsSelector(o.column) && c.jsonColumnKey(o.column) == key {
					group = append(group, o)
				}
			}
			if written {
				c.write(", ")
			}
			if err := c.jsonUpdateGroup(group); err != nil {
				return err
			}
			written = true
			continue
		}
		if written {
			c.write(", ")
		}
		c.write(c.setColumn(s.column) + " = ")
		if err := c.value(s.value); err != nil {
			return err
		}
		written = true
	}
	return nil
}

func (c *compiler) whereSuffix(b *Builder) error {
	if len(b.wheres) == 0 {
		return nil
	}
	c.write(" WHERE ")
	return c.clauses(b.wheres)
}

func (c *compiler) orderLimitSuffix(b *Builder) error {
	return c.orderLimitOffset(b.orders, b.limit, nil)
}

// rowIDColumn is the physical row locator used to emulate joined/limited writes.
func (c *compiler) rowIDColumn() string {
	if c.d == Postgres {
		return "ctid"
	}
	return "rowid"
}

// rowIDSubquery writes `WHERE "ctid" IN (SELECT alias.ctid FROM ... )` for
// Postgres/SQLite updates and deletes that carry joins or a limit.
func (c *compiler) rowIDSubquery(b *Builder) error {
	alias := b.table
	if parts := tableAliasRe.Split(b.table, -1); len(parts) > 1 {
		alias = parts[len(parts)-1]
	}
	sub := b.Clone()
	sub.columns = []selectItem{{column: alias + "." + c.rowIDColumn()}}
	sub.returning = nil
	c.write(" WHERE " + c.g.Quote(c.rowIDColumn()) + " IN ")
	return c.subquery(sub)
}

// joinedOrderLimit rejects ORDER BY and LIMIT on a MySQL-family UPDATE or DELETE
// with joins: the multiple-table syntax does not accept them, and dropping the
// LIMIT would widen the write (the limit may be what satisfied requireWhere).
func (c *compiler) joinedOrderLimit(b *Builder, stmt string) error {
	if c.d.mysqlFamily() && len(b.joins) > 0 && (b.limit != nil || len(b.orders) > 0) {
		return fmt.Errorf("%w: order by or limit on %s with joins on %s", ErrUnsupported, stmt, c.d)
	}
	return nil
}

func (c *compiler) update(b *Builder, sets []setItem) error {
	if err := c.joinedOrderLimit(b, "update"); err != nil {
		return err
	}
	c.write("UPDATE " + c.wrapTable(b.table))
	viaRowID := !c.d.mysqlFamily() && (len(b.joins) > 0 || b.limit != nil)
	if c.d.mysqlFamily() {
		for _, j := range b.joins {
			c.write(" ")
			if err := c.join(j); err != nil {
				return err
			}
		}
	}
	c.write(" SET ")
	if err := c.setList(sets); err != nil {
		return err
	}
	if viaRowID {
		if err := c.rowIDSubquery(b); err != nil {
			return err
		}
	} else {
		if err := c.whereSuffix(b); err != nil {
			return err
		}
		if c.d.mysqlFamily() {
			if err := c.orderLimitSuffix(b); err != nil {
				return err
			}
		}
	}
	return c.returningClause(b.returning)
}

// UpdateSQL builds "UPDATE t SET a = ?, ... WHERE ...". Columns are sorted.
// Values may be Raw expressions or *Builder subqueries. Joins use native
// UPDATE ... JOIN on MySQL and a ctid/rowid subquery on Postgres/SQLite.
// Refuses to build without a where clause or limit unless AllRows() was called.
//
// Example:
//
//	conn.Table("users").Where("id", 7).UpdateSQL(map[string]any{"name": "Ann"})
//	// UPDATE "users" SET "name" = $1 WHERE "id" = $2   args: [Ann 7]
func (b *Builder) UpdateSQL(values map[string]any) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	if len(values) == 0 {
		return "", nil, ErrNoValues
	}
	if err := b.requireWhere(); err != nil {
		return "", nil, err
	}
	return c.result(c.update(b, sortedSets(values)))
}

// UpdateFromSQL builds a Postgres "UPDATE t SET ... FROM j1, j2 WHERE <join conditions> AND <wheres>",
// where each Join's ON conditions become part of the WHERE. Other dialects return
// ErrUnsupported (use UpdateSQL with joins on MySQL).
//
// Example:
//
//	conn.Table("posts").Join("users", "users.id", "=", "posts.user_id").
//		Where("users.banned", true).UpdateFromSQL(map[string]any{"hidden": true})
//	// UPDATE "posts" SET "hidden" = $1 FROM "users"
//	//   WHERE "users"."banned" = $2 AND "users"."id" = "posts"."user_id"   args: [true true]
func (b *Builder) UpdateFromSQL(values map[string]any) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	if c.d != Postgres {
		return "", nil, fmt.Errorf("%w: UpdateFrom on %s", ErrUnsupported, c.d)
	}
	if len(values) == 0 {
		return "", nil, ErrNoValues
	}
	if len(b.wheres) == 0 && len(b.joins) == 0 && !b.allRows {
		return "", nil, ErrNoWhere
	}
	c.write("UPDATE " + c.wrapTable(b.table) + " SET ")
	if err := c.setList(sortedSets(values)); err != nil {
		return "", nil, err
	}
	// The wheres and each join's ON conditions are separate predicates ANDed
	// together; isolated keeps an OR inside one from spilling into the others.
	conds := isolated(b.wheres)
	if len(b.joins) > 0 {
		tables := make([]string, len(b.joins))
		for i, j := range b.joins {
			if j.sub != nil {
				return "", nil, fmt.Errorf("%w: subquery joins in UpdateFrom", ErrUnsupported)
			}
			tables[i] = c.wrapTable(j.table)
			conds = append(slices.Clip(conds), isolated(j.on)...)
		}
		c.write(" FROM " + strings.Join(tables, ", "))
	}
	if len(conds) > 0 {
		c.write(" WHERE ")
		if err := c.clauses(conds); err != nil {
			return "", nil, err
		}
	}
	return c.result(c.returningClause(b.returning))
}

func isNumber(v any) bool {
	switch reflect.ValueOf(v).Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}

// adjust builds "col = col <op> ?" updates for increment/decrement; extra columns
// are set verbatim and win over an adjusted column of the same name.
func (b *Builder) adjust(op string, amounts map[string]any, extra map[string]any) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	if len(amounts) == 0 {
		return "", nil, ErrNoValues
	}
	if err := b.requireWhere(); err != nil {
		return "", nil, err
	}
	var sets []setItem
	for _, it := range sortedSets(amounts) {
		if _, overridden := extra[it.column]; overridden {
			continue
		}
		if !isNumber(it.value) {
			return "", nil, fmt.Errorf("%w: amount for %q must be numeric, got %T", ErrInvalidArgs, it.column, it.value)
		}
		sets = append(sets, setItem{it.column, Raw(c.wrapColumn(it.column)+" "+op+" ?", it.value)})
	}
	sets = append(sets, sortedSets(extra)...)
	return c.result(c.update(b, sets))
}

// IncrementSQL builds "UPDATE t SET col = col + ? [, extra...] WHERE ...".
//
// Example:
//
//	conn.Table("posts").Where("id", 7).IncrementSQL("views", 1, map[string]any{"seen_by": "ann"})
//	// UPDATE "posts" SET "views" = "views" + $1, "seen_by" = $2 WHERE "id" = $3   args: [1 ann 7]
func (b *Builder) IncrementSQL(column string, amount any, extra map[string]any) (string, []any, error) {
	return b.adjust("+", map[string]any{column: amount}, extra)
}

// DecrementSQL builds "UPDATE t SET col = col - ? [, extra...] WHERE ...".
//
// Example:
//
//	conn.Table("products").Where("id", 3).DecrementSQL("stock", 2, nil)
//	// UPDATE "products" SET "stock" = "stock" - $1 WHERE "id" = $2   args: [2 3]
func (b *Builder) DecrementSQL(column string, amount any, extra map[string]any) (string, []any, error) {
	return b.adjust("-", map[string]any{column: amount}, extra)
}

// IncrementEachSQL increments several columns at once; amounts maps column to amount.
//
// Example:
//
//	conn.Table("stats").Where("day", "2026-01-01").
//		IncrementEachSQL(map[string]any{"views": 5, "clicks": 1}, nil)
//	// UPDATE "stats" SET "clicks" = "clicks" + $1, "views" = "views" + $2 WHERE "day" = $3
//	// args: [1 5 2026-01-01]
func (b *Builder) IncrementEachSQL(amounts, extra map[string]any) (string, []any, error) {
	return b.adjust("+", amounts, extra)
}

// DecrementEachSQL decrements several columns at once.
//
// Example:
//
//	conn.Table("stats").Where("day", "2026-01-01").
//		DecrementEachSQL(map[string]any{"views": 5, "clicks": 1}, nil)
//	// UPDATE "stats" SET "clicks" = "clicks" - $1, "views" = "views" - $2 WHERE "day" = $3
//	// args: [1 5 2026-01-01]
func (b *Builder) DecrementEachSQL(amounts, extra map[string]any) (string, []any, error) {
	return b.adjust("-", amounts, extra)
}

// ---- delete / truncate ----

func (c *compiler) delete(b *Builder) error {
	if err := c.joinedOrderLimit(b, "delete"); err != nil {
		return err
	}
	switch {
	case !c.d.mysqlFamily() && (len(b.joins) > 0 || b.limit != nil):
		c.write("DELETE FROM " + c.wrapTable(b.table))
		if err := c.rowIDSubquery(b); err != nil {
			return err
		}
	case c.d.mysqlFamily() && len(b.joins) > 0:
		alias := c.wrapTable(b.table)
		if i := strings.LastIndex(strings.ToLower(alias), " as "); i >= 0 {
			alias = alias[i+4:]
		}
		c.write("DELETE " + alias + " FROM " + c.wrapTable(b.table))
		for _, j := range b.joins {
			c.write(" ")
			if err := c.join(j); err != nil {
				return err
			}
		}
		if err := c.whereSuffix(b); err != nil {
			return err
		}
	default:
		c.write("DELETE FROM " + c.wrapTable(b.table))
		if err := c.whereSuffix(b); err != nil {
			return err
		}
		if c.d.mysqlFamily() {
			if err := c.orderLimitSuffix(b); err != nil {
				return err
			}
		}
	}
	return c.returningClause(b.returning)
}

// DeleteSQL builds "DELETE FROM t WHERE ...". Joins use "DELETE alias FROM ... JOIN"
// on MySQL and a ctid/rowid subquery on Postgres/SQLite; MySQL also honors
// order/limit. Refuses to build without a where clause or limit unless
// AllRows() was called (use TruncateSQL to empty a table).
//
// Example:
//
//	conn.Table("users").Where("id", 7).DeleteSQL()
//	// DELETE FROM "users" WHERE "id" = $1   args: [7]
func (b *Builder) DeleteSQL() (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	if err := b.requireWhere(); err != nil {
		return "", nil, err
	}
	return c.result(c.delete(b))
}

// sqliteSchema is the quoted `"schema".` prefix of a "schema.table" name, or "".
func (c *compiler) sqliteSchema(table string) string {
	if i := strings.LastIndex(table, "."); i >= 0 {
		return c.g.Quote(table[:i]) + "."
	}
	return ""
}

// TruncateSQL builds the statements that empty the table and reset its identity.
// Postgres: "TRUNCATE t RESTART IDENTITY" (no CASCADE),
// so referencing tables are never emptied implicitly. MySQL: "TRUNCATE TABLE t".
// SQLite has no TRUNCATE: it deletes the sqlite_sequence row, then every row.
// Truncate runs both in one transaction and skips the first when the database has
// no sqlite_sequence table yet (no table uses AUTOINCREMENT); run TruncateSQL's
// statements yourself only if you handle that case.
//
// Example:
//
//	stmts, err := conn.Table("users").TruncateSQL()
//	// Postgres: TRUNCATE "users" RESTART IDENTITY
//	// MySQL:    TRUNCATE TABLE `users`
//	// SQLite:   DELETE FROM sqlite_sequence WHERE name = ?  args: [users]
//	//           DELETE FROM "users"
func (b *Builder) TruncateSQL() ([]Stmt, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return nil, err
	}
	switch c.d {
	case Postgres:
		return []Stmt{{SQL: "TRUNCATE " + c.wrapTable(b.table) + " RESTART IDENTITY"}}, nil
	case MySQL, MariaDB:
		return []Stmt{{SQL: "TRUNCATE TABLE " + c.wrapTable(b.table)}}, nil
	}
	schema, name := c.sqliteSchema(b.table), b.table
	if i := strings.LastIndex(b.table, "."); i >= 0 {
		name = b.table[i+1:]
	}
	return []Stmt{
		{SQL: "DELETE FROM " + schema + "sqlite_sequence WHERE name = ?", Args: []any{b.prefix + name}},
		{SQL: "DELETE FROM " + c.wrapTable(b.table)},
	}, nil
}
