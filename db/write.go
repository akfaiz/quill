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

// adjustment is a SET value that adds to or subtracts from the column itself:
// "col = col + ?".
type adjustment struct {
	op     string // "+" or "-"
	amount any
}

var tableAliasRe = regexp.MustCompile(`(?i)\s+as\s+`)

// Returning adds a RETURNING clause to insert/update/delete/upsert
// (Postgres, SQLite >= 3.35; MySQL returns ErrUnsupported). "*" is allowed.
//
// Example:
//
//	conn.Insert("users").Values(map[string]any{"name": "Ann"}).Returning("id", "created_at").ToSQL()
//	// INSERT INTO "users" ("name") VALUES ($1) RETURNING "id", "created_at"   args: [Ann]
//	// MySQL: ToSQL returns ErrUnsupported
func (b *query) Returning(columns ...string) *query {
	b.returning = append(b.returning, columns...)
	return b
}

// AllRows confirms that an update/delete without a where clause is intended.
// Without it, an UpdateBuilder or DeleteBuilder refuses to build (ErrNoWhere).
//
// Example:
//
//	conn.Delete("sessions").AllRows().ToSQL()
//	// DELETE FROM "sessions"   args: []
func (b *query) AllRows() *query { b.allRows = true; return b }

func (b *query) writeCompiler() (*compiler, error) {
	c, err := b.newCompiler()
	if err != nil {
		return nil, err
	}
	if b.table == "" {
		return nil, ErrNoTable
	}
	return c, nil
}

// rejectWith refuses a WITH clause on a statement that cannot carry one.
func (b *query) rejectWith(stmt string) error {
	if len(b.ctes) > 0 {
		return fmt.Errorf("%w: with clause on %s", ErrUnsupported, stmt)
	}
	return nil
}

// withWrite writes the WITH prefix of an UPDATE or DELETE. MariaDB has no WITH before them.
func (c *compiler) withWrite(b *query, stmt string) error {
	if len(b.ctes) > 0 && c.d == MariaDB {
		return fmt.Errorf("%w: with clause on %s on %s", ErrUnsupported, stmt, c.d)
	}
	return c.with(b)
}

func (b *query) requireWhere() error {
	if len(b.wheres) == 0 && b.limit == nil && !b.allRows {
		return ErrNoWhere
	}
	return nil
}

// reserveWrite sizes the buffer and the args for an UPDATE/DELETE with nSets assignments,
// as reserve does for SELECT. The numbers are estimates: undershooting only costs a regrow.
func (c *compiler) reserveWrite(b *query, nSets int) {
	c.sb.Grow(48 + 20*nSets + 24*len(b.wheres))
	if n := nSets + len(b.wheres); n > 0 && c.args == nil {
		c.args = make([]any, 0, n)
	}
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

// writeColumnList writes quoted, comma separated columns straight into the output buffer.
func (c *compiler) writeColumnList(cols []string) {
	for i, col := range cols {
		if i > 0 {
			c.write(", ")
		}
		c.writeColumn(col)
	}
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

func (c *compiler) insert(b *query, rows []map[string]any, ignore bool) error {
	cols, err := normalizeRows(rows)
	if err != nil {
		return err
	}
	c.sb.Grow(48 + len(cols)*12 + len(rows)*len(cols)*6)
	c.write(c.insertVerb(ignore))
	c.writeTable(b.table)
	if len(cols) == 0 {
		if c.d.mysqlFamily() {
			c.write(" () VALUES ()")
		} else {
			c.write(" DEFAULT VALUES")
		}
	} else {
		c.write(" (")
		c.writeColumnList(cols)
		c.write(") VALUES ")
		if c.args == nil {
			c.args = make([]any, 0, len(rows)*len(cols))
		}
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

// insertSQL compiles INSERT INTO t (cols) VALUES (...), (...) for DB.Insert.
func (b *query) insertSQL(rows ...map[string]any) (string, []any, error) {
	if err := b.rejectWith("an insert without Using"); err != nil {
		return "", nil, err
	}
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insert(b, rows, false))
}

// insertOrIgnoreSQL compiles an insert that skips conflicting rows (InsertBuilder.Ignore).
func (b *query) insertOrIgnoreSQL(rows ...map[string]any) (string, []any, error) {
	if err := b.rejectWith("an insert without Using"); err != nil {
		return "", nil, err
	}
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insert(b, rows, true))
}

// insertGetIDSQL compiles a single-row insert; Postgres appends RETURNING "<sequence>"
// (default "id"), MySQL/SQLite read LastInsertId (InsertBuilder.IDSQL).
func (b *query) insertGetIDSQL(row map[string]any, sequence ...string) (string, []any, error) {
	if b.dialect == Postgres {
		seq := "id"
		if len(sequence) > 0 && sequence[0] != "" {
			seq = sequence[0]
		}
		b = b.Clone()
		b.returning = []string{seq}
	}
	return b.insertSQL(row)
}

func (c *compiler) insertUsing(b *query, columns []string, sub *query, ignore bool) error {
	if sub == nil {
		return fmt.Errorf("%w: nil subquery", ErrInvalidArgs)
	}
	if sub.err != nil {
		return sub.err
	}
	if len(b.ctes) > 0 {
		// WITH cannot precede INSERT on MySQL: move it into the SELECT, where every dialect accepts it
		merged := sub.Clone()
		merged.ctes = append(cloneCTEs(b.ctes), merged.ctes...)
		if dup := duplicateCTE(merged.ctes); dup != "" {
			return fmt.Errorf("%w: duplicate CTE name %q", ErrInvalidArgs, dup)
		}
		sub = merged
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

// insertUsingSQL compiles INSERT INTO t (cols) SELECT ... (InsertBuilder.Using).
func (b *query) insertUsingSQL(columns []string, sub *query) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insertUsing(b, columns, sub, false))
}

// insertOrIgnoreUsingSQL is insertUsingSQL that skips conflicting rows.
func (b *query) insertOrIgnoreUsingSQL(columns []string, sub *query) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	return c.result(c.insertUsing(b, columns, sub, true))
}

// ---- upsert ----

// upsertSQL compiles an insert that updates on conflict (InsertBuilder.Upsert). update
// lists the columns that take the inserted value (nil means every inserted column);
// uniqueBy is the conflict target on Postgres/SQLite and ignored on MySQL.
func (b *query) upsertSQL(rows []map[string]any, uniqueBy, update []string) (string, []any, error) {
	if err := b.rejectWith("an upsert"); err != nil {
		return "", nil, err
	}
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

func (b *query) withoutReturning() *query {
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
	slices.SortFunc(items, func(a, b setItem) int { return strings.Compare(a.column, b.column) })
	return items
}

// setColumn wraps an update target. Postgres/SQLite reject qualified SET
// targets, so only the last segment is kept; MySQL keeps the qualifier
// (needed for multi-table updates).
func (c *compiler) setColumnTo(sb *strings.Builder, key string) {
	if !c.d.mysqlFamily() {
		if i := strings.LastIndex(key, "."); i >= 0 {
			key = key[i+1:]
		}
	}
	c.columnTo(sb, key)
}

func (c *compiler) setList(sets []setItem) error {
	written := false
	var done map[string]bool // only needed for JSON selectors
	for _, s := range sets {
		if jsonpath.IsSelector(s.column) {
			key := c.jsonColumnKey(s.column)
			if done[key] {
				continue
			}
			if done == nil {
				done = map[string]bool{}
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
		c.setColumnTo(&c.sb, s.column)
		c.write(" = ")
		v := s.value
		if a, ok := v.(adjustment); ok {
			v = Raw(c.wrapColumn(s.column)+" "+a.op+" ?", a.amount)
		}
		if err := c.value(v); err != nil {
			return err
		}
		written = true
	}
	return nil
}

func (c *compiler) whereSuffix(b *query) error {
	if len(b.wheres) == 0 {
		return nil
	}
	c.write(" WHERE ")
	return c.clauses(b.wheres)
}

func (c *compiler) orderLimitSuffix(b *query) error {
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
func (c *compiler) rowIDSubquery(b *query) error {
	alias := b.table
	if parts := tableAliasRe.Split(b.table, -1); len(parts) > 1 {
		alias = parts[len(parts)-1]
	}
	sub := b.Clone()
	sub.columns = []selectItem{{column: alias + "." + c.rowIDColumn()}}
	sub.returning = nil
	sub.ctes = nil // the statement's own WITH already precedes the verb and is visible here
	c.write(" WHERE " + c.g.Quote(c.rowIDColumn()) + " IN ")
	return c.subquery(sub)
}

// joinedOrderLimit rejects ORDER BY and LIMIT on a MySQL-family UPDATE or DELETE
// with joins: the multiple-table syntax does not accept them, and dropping the
// LIMIT would widen the write (the limit may be what satisfied requireWhere).
func (c *compiler) joinedOrderLimit(b *query, stmt string) error {
	if c.d.mysqlFamily() && len(b.joins) > 0 && (b.limit != nil || len(b.orders) > 0) {
		return fmt.Errorf("%w: order by or limit on %s with joins on %s", ErrUnsupported, stmt, c.d)
	}
	return nil
}

func (c *compiler) update(b *query, sets []setItem) error {
	if err := c.joinedOrderLimit(b, "update"); err != nil {
		return err
	}
	if err := c.withWrite(b, "update"); err != nil {
		return err
	}
	c.write("UPDATE ")
	c.writeTable(b.table)
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

// updateSQL compiles UPDATE t SET ... WHERE ... (UpdateBuilder.ToSQL). Joins use native
// UPDATE ... JOIN on MySQL and a ctid/rowid subquery on Postgres/SQLite. Refuses to
// build without a where clause or limit unless AllRows was called.
func (b *query) updateSQL(sets []setItem) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	if len(sets) == 0 {
		return "", nil, ErrNoValues
	}
	if err := b.requireWhere(); err != nil {
		return "", nil, err
	}
	c.reserveWrite(b, len(sets))
	return c.result(c.update(b, sets))
}

// updateFromSQL compiles a Postgres UPDATE t SET ... FROM j1, j2 WHERE <join conditions> AND <wheres>
// (UpdateBuilder.FromJoins); each Join's ON conditions become part of the WHERE.
// Other dialects return ErrUnsupported.
func (b *query) updateFromSQL(sets []setItem) (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	if c.d != Postgres {
		return "", nil, fmt.Errorf("%w: UpdateFrom on %s", ErrUnsupported, c.d)
	}
	if len(sets) == 0 {
		return "", nil, ErrNoValues
	}
	if len(b.wheres) == 0 && len(b.joins) == 0 && !b.allRows {
		return "", nil, ErrNoWhere
	}
	if err := c.withWrite(b, "update"); err != nil {
		return "", nil, err
	}
	c.write("UPDATE ")
	c.writeTable(b.table)
	c.write(" SET ")
	if err := c.setList(sets); err != nil {
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

// ---- delete / truncate ----

func (c *compiler) delete(b *query) error {
	if err := c.joinedOrderLimit(b, "delete"); err != nil {
		return err
	}
	if err := c.withWrite(b, "delete"); err != nil {
		return err
	}
	switch {
	case !c.d.mysqlFamily() && (len(b.joins) > 0 || b.limit != nil):
		c.write("DELETE FROM ")
		c.writeTable(b.table)
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
		c.write("DELETE FROM ")
		c.writeTable(b.table)
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

// deleteSQL compiles DELETE FROM t WHERE ... (DeleteBuilder.ToSQL). Joins use
// "DELETE alias FROM ... JOIN" on MySQL and a ctid/rowid subquery on Postgres/SQLite;
// MySQL also honors order/limit. Refuses to build without a where clause or limit
// unless AllRows was called (use DB.Truncate to empty a table).
func (b *query) deleteSQL() (string, []any, error) {
	c, err := b.writeCompiler()
	if err != nil {
		return "", nil, err
	}
	if err := b.requireWhere(); err != nil {
		return "", nil, err
	}
	c.reserveWrite(b, 0)
	return c.result(c.delete(b))
}

// sqliteSchema is the quoted `"schema".` prefix of a "schema.table" name, or "".
func (c *compiler) sqliteSchema(table string) string {
	if i := strings.LastIndex(table, "."); i >= 0 {
		return c.g.Quote(table[:i]) + "."
	}
	return ""
}

// truncateSQL builds the statements that empty the table and reset its identity.
// Postgres: "TRUNCATE t RESTART IDENTITY" (no CASCADE), so referencing tables are
// never emptied implicitly. MySQL: "TRUNCATE TABLE t". SQLite has no TRUNCATE: it
// deletes the sqlite_sequence row, then every row. DB.Truncate runs them in one
// transaction and skips the first when the database has no sqlite_sequence table
// yet (no table uses AUTOINCREMENT); run DB.TruncateSQL's statements yourself only
// if you handle that case.
func (b *query) truncateSQL() ([]Stmt, error) {
	if err := b.rejectWith("truncate"); err != nil {
		return nil, err
	}
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
