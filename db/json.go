package db

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/akfaiz/quill/internal/jsonpath"
)

// jsonFieldAndPath splits "col->a->b" into the wrapped column and ", '$.\"a\".\"b\"'".
// Used by MySQL and SQLite.
func (c *compiler) jsonFieldAndPath(col string) (field, path string) {
	parts := strings.SplitN(col, "->", 2)
	field = c.wrapColumn(parts[0])
	if len(parts) > 1 {
		path = ", " + jsonpath.Path(parts[1])
	}
	return field, path
}

// wrapJSONSelector renders "col->a->b" as a text-valued selector.
func (c *compiler) wrapJSONSelector(value string) string {
	switch c.d {
	case MySQL, MariaDB:
		// MariaDB's json_value would return NULL for arrays and objects, so it uses
		// MySQL's form, which returns the JSON text like every other dialect.
		field, path := c.jsonFieldAndPath(value)
		return "JSON_UNQUOTE(JSON_EXTRACT(" + field + path + "))"
	case SQLite:
		field, path := c.jsonFieldAndPath(value)
		return "JSON_EXTRACT(" + field + path + ")"
	}
	parts := strings.Split(value, "->")
	field := c.wrapColumn(parts[0])
	attrs := jsonpath.PgAttributes(parts[1:], "'")
	if len(attrs) == 0 {
		return field
	}
	last := attrs[len(attrs)-1]
	if len(attrs) > 1 {
		return field + "->" + strings.Join(attrs[:len(attrs)-1], "->") + "->>" + last
	}
	return field + "->>" + last
}

// wrapJSONBooleanSelector renders a selector that compares as JSON, not text.
func (c *compiler) wrapJSONBooleanSelector(value string) string {
	switch c.d {
	case MySQL, MariaDB:
		field, path := c.jsonFieldAndPath(value)
		return "JSON_EXTRACT(" + field + path + ")"
	case SQLite:
		return c.wrapJSONSelector(value)
	}
	return "(" + strings.ReplaceAll(c.wrapJSONSelector(value), "->>", "->") + ")::JSONB"
}

// pgJSONColumn wraps a selector for jsonb operators: the final "->>" becomes "->".
func (c *compiler) pgJSONColumn(col string) string {
	return strings.ReplaceAll(c.wrapColumn(col), "->>", "->")
}

// jsonBinding encodes a value for json_contains/@>; SQLite compares the raw value.
func (c *compiler) jsonBinding(v any) (any, error) {
	if c.d == SQLite {
		return v, nil
	}
	return jsonpath.Encode(v)
}

// jsonValue writes a JSON-encoded bind arg, or an Expression inline.
func (c *compiler) jsonValue(v any) error {
	if _, ok := v.(Expression); ok {
		return c.value(v)
	}
	enc, err := c.jsonBinding(v)
	if err != nil {
		return err
	}
	c.param(enc)
	return nil
}

func notKw(not bool) string {
	if not {
		return "NOT "
	}
	return ""
}

func (c *compiler) jsonClause(cl clause) error {
	switch cl.kind {
	case clauseJSONBool:
		c.write(c.wrapJSONBooleanSelector(cl.column) + " " + cl.op + " ")
		lit := "FALSE"
		if cl.value.(bool) {
			lit = "TRUE"
		}
		if c.d == Postgres {
			lit = "'" + strings.ToLower(lit) + "'::JSONB" // a jsonb literal is lowercase JSON text
		}
		c.write(lit)
	case clauseJSONContains:
		return c.jsonContains(cl)
	case clauseJSONOverlaps:
		if !c.d.mysqlFamily() {
			return fmt.Errorf("%w: JSON overlaps on %s", ErrUnsupported, c.d)
		}
		field, path := c.jsonFieldAndPath(cl.column)
		if path != "" {
			field = "JSON_EXTRACT(" + field + path + ")"
		}
		c.write(notKw(cl.not) + "JSON_OVERLAPS(" + field + ", ")
		if err := c.jsonValue(cl.value); err != nil {
			return err
		}
		c.write(")")
	case clauseJSONContainsKey:
		return c.jsonContainsKey(cl)
	case clauseJSONLength:
		return c.jsonLength(cl)
	}
	return nil
}

func (c *compiler) jsonContains(cl clause) error {
	c.write(notKw(cl.not))
	switch c.d {
	case MySQL, MariaDB:
		field, path := c.jsonFieldAndPath(cl.column)
		c.write("JSON_CONTAINS(" + field + ", ")
		if err := c.jsonValue(cl.value); err != nil {
			return err
		}
		c.write(path + ")")
	case SQLite:
		field, path := c.jsonFieldAndPath(cl.column)
		c.write("EXISTS (SELECT 1 FROM JSON_EACH(" + field + path + ") WHERE " +
			c.g.Quote("json_each") + "." + c.g.Quote("value") + " IS ")
		if err := c.jsonValue(cl.value); err != nil {
			return err
		}
		c.write(")")
	default:
		c.write("(" + c.pgJSONColumn(cl.column) + ")::JSONB @> ")
		return c.jsonValue(cl.value)
	}
	return nil
}

func (c *compiler) jsonContainsKey(cl clause) error {
	c.write(notKw(cl.not))
	switch c.d {
	case MySQL, MariaDB:
		field, path := c.jsonFieldAndPath(cl.column)
		c.write("IFNULL(JSON_CONTAINS_PATH(" + field + ", 'one'" + path + "), 0)")
	case SQLite:
		field, path := c.jsonFieldAndPath(cl.column)
		c.write("JSON_TYPE(" + field + path + ") IS NOT NULL")
	default:
		segs := strings.Split(cl.column, "->")
		if len(segs) < 2 {
			return fmt.Errorf("%w: JSON contains-key on postgres needs a path (col->key)", ErrInvalidArgs)
		}
		last := segs[len(segs)-1]
		segs = segs[:len(segs)-1]
		key, index, hasIndex := jsonpath.ParseIndexSegment(last)
		if hasIndex && key != "" {
			segs = append(segs, key)
		}
		col := strings.ReplaceAll(c.wrapColumn(strings.Join(segs, "->")), "->>", "->")
		if hasIndex {
			n := index + 1
			if index < 0 {
				n = -index
			}
			c.write("CASE WHEN JSONB_TYPEOF((" + col + ")::JSONB) = 'array' THEN JSONB_ARRAY_LENGTH((" +
				col + ")::JSONB) >= " + strconv.Itoa(n) + " ELSE FALSE END")
			return nil
		}
		// a bare "?" is the jsonb key-exists operator; it is not a bind marker.
		c.write("COALESCE((" + col + ")::JSONB ? '" + strings.ReplaceAll(last, "'", "''") + "', FALSE)")
	}
	return nil
}

func (c *compiler) jsonLength(cl clause) error {
	switch c.d {
	case MySQL, MariaDB:
		field, path := c.jsonFieldAndPath(cl.column)
		c.write("JSON_LENGTH(" + field + path + ") " + cl.op + " ")
	case SQLite:
		field, path := c.jsonFieldAndPath(cl.column)
		c.write("JSON_ARRAY_LENGTH(" + field + path + ") " + cl.op + " ")
	default:
		c.write("JSONB_ARRAY_LENGTH((" + c.pgJSONColumn(cl.column) + ")::JSONB) " + cl.op + " ")
	}
	return c.value(cl.value)
}

// ---- builder API ----

func (b *Builder) jsonContains(boolean, column string, value any, not bool) *Builder {
	return b.push(&b.wheres, boolean, clause{kind: clauseJSONContains, column: column, value: value, not: not})
}

// WhereJsonContains adds a JSON containment test on a column or "col->path".
// MySQL json_contains, Postgres @> (jsonb), SQLite json_each membership.
func (b *Builder) WhereJsonContains(column string, value any) *Builder {
	return b.jsonContains("and", column, value, false)
}

// OrWhereJsonContains is WhereJsonContains joined with OR.
func (b *Builder) OrWhereJsonContains(column string, value any) *Builder {
	return b.jsonContains("or", column, value, false)
}

// WhereJsonDoesntContain negates WhereJsonContains.
func (b *Builder) WhereJsonDoesntContain(column string, value any) *Builder {
	return b.jsonContains("and", column, value, true)
}

// OrWhereJsonDoesntContain is WhereJsonDoesntContain joined with OR.
func (b *Builder) OrWhereJsonDoesntContain(column string, value any) *Builder {
	return b.jsonContains("or", column, value, true)
}

func (b *Builder) jsonOverlaps(boolean, column string, value any, not bool) *Builder {
	return b.push(&b.wheres, boolean, clause{kind: clauseJSONOverlaps, column: column, value: value, not: not})
}

// WhereJsonOverlaps adds json_overlaps (MySQL only; other dialects error at compile time).
func (b *Builder) WhereJsonOverlaps(column string, value any) *Builder {
	return b.jsonOverlaps("and", column, value, false)
}

// OrWhereJsonOverlaps is WhereJsonOverlaps joined with OR.
func (b *Builder) OrWhereJsonOverlaps(column string, value any) *Builder {
	return b.jsonOverlaps("or", column, value, false)
}

// WhereJsonDoesntOverlap negates WhereJsonOverlaps.
func (b *Builder) WhereJsonDoesntOverlap(column string, value any) *Builder {
	return b.jsonOverlaps("and", column, value, true)
}

// OrWhereJsonDoesntOverlap is WhereJsonDoesntOverlap joined with OR.
func (b *Builder) OrWhereJsonDoesntOverlap(column string, value any) *Builder {
	return b.jsonOverlaps("or", column, value, true)
}

func (b *Builder) jsonContainsKey(boolean, column string, not bool) *Builder {
	return b.push(&b.wheres, boolean, clause{kind: clauseJSONContainsKey, column: column, not: not})
}

// WhereJsonContainsKey tests that the JSON path "col->key" exists.
func (b *Builder) WhereJsonContainsKey(column string) *Builder {
	return b.jsonContainsKey("and", column, false)
}

// OrWhereJsonContainsKey is WhereJsonContainsKey joined with OR.
func (b *Builder) OrWhereJsonContainsKey(column string) *Builder {
	return b.jsonContainsKey("or", column, false)
}

// WhereJsonDoesntContainKey negates WhereJsonContainsKey.
func (b *Builder) WhereJsonDoesntContainKey(column string) *Builder {
	return b.jsonContainsKey("and", column, true)
}

// OrWhereJsonDoesntContainKey is WhereJsonDoesntContainKey joined with OR.
func (b *Builder) OrWhereJsonDoesntContainKey(column string) *Builder {
	return b.jsonContainsKey("or", column, true)
}

func (b *Builder) jsonLength(boolean, column string, args []any) *Builder {
	op, val, err := parseOpValue(column, args)
	if err != nil {
		return b.fail(err)
	}
	if val == nil {
		return b.fail(fmt.Errorf("%w: JSON length needs a non-nil value", ErrInvalidArgs))
	}
	if _, ok := val.(Expression); !ok {
		n, err := toInt64(val)
		if err != nil {
			return b.fail(fmt.Errorf("%w: JSON length must be an integer: %w", ErrInvalidArgs, err))
		}
		val = n
	}
	return b.push(&b.wheres, boolean, clause{kind: clauseJSONLength, column: column, op: op, value: val})
}

// WhereJsonLength compares the length of a JSON array: (value) or (operator, value).
func (b *Builder) WhereJsonLength(column string, args ...any) *Builder {
	return b.jsonLength("and", column, args)
}

// OrWhereJsonLength is WhereJsonLength joined with OR.
func (b *Builder) OrWhereJsonLength(column string, args ...any) *Builder {
	return b.jsonLength("or", column, args)
}

// ---- JSON path updates ----

// jsonColumnKey identifies the column a "col->path" key updates, so several
// paths of one column fold into a single assignment (Postgres rejects repeated targets).
func (c *compiler) jsonColumnKey(key string) string {
	col, _ := jsonpath.SplitKey(key)
	if c.d.mysqlFamily() {
		return col
	}
	return jsonpath.LastSegment(col)
}

func (c *compiler) mysqlJSONValue(value any) error {
	switch v := value.(type) {
	case bool:
		c.write(strings.ToUpper(strconv.FormatBool(v)))
	case Expression:
		c.raw(v)
	default:
		if !jsonpath.IsContainer(value) {
			c.param(value)
			return nil
		}
		enc, err := jsonpath.Encode(value)
		if err != nil {
			return err
		}
		if c.d == MariaDB { // MariaDB has no CAST AS JSON
			c.write("JSON_QUERY(")
			c.param(enc)
			c.write(", '$')")
			return nil
		}
		c.write("CAST(")
		c.param(enc)
		c.write(" AS JSON)")
	}
	return nil
}

// jsonUpdateGroup writes one assignment covering every "col->path" item of a
// column: MySQL json_set(col, p1, v1, p2, v2), Postgres nested jsonb_set. SQLite is
// handled by groupSQLiteJSON/jsonPatch instead.
func (c *compiler) jsonUpdateGroup(items []setItem) error {
	if c.d.mysqlFamily() {
		field, _ := c.jsonFieldAndPath(items[0].column)
		c.write(field + " = JSON_SET(" + field)
		for _, it := range items {
			_, p := c.jsonFieldAndPath(it.column)
			c.write(p + ", ")
			if err := c.mysqlJSONValue(it.value); err != nil {
				return err
			}
		}
		c.write(")")
		return nil
	}
	col, _ := jsonpath.SplitKey(items[0].column)
	field := c.wrapColumn(jsonpath.LastSegment(col))
	c.write(field + " = " + strings.Repeat("JSONB_SET(", len(items)) + field + "::JSONB")
	for _, it := range items {
		_, path := jsonpath.SplitKey(it.column)
		c.write(", '{" + strings.Join(jsonpath.PgAttributes(path, `"`), ",") + "}', ")
		if e, ok := it.value.(Expression); ok {
			c.raw(e)
		} else {
			enc, err := jsonpath.Encode(it.value)
			if err != nil {
				return err
			}
			c.param(enc)
		}
		c.write(")")
	}
	return nil
}

// jsonPatch is the SQLite value for all "col->path" updates of one column.
type jsonPatch struct{ doc map[string]any }

// groupSQLiteJSON folds "col->a->b" sets into one json_patch per column,
// placed after the plain sets (as Laravel does).
func groupSQLiteJSON(sets []setItem) ([]setItem, error) {
	var plain []setItem
	groups := map[string]map[string]any{}
	for _, s := range sets {
		if !jsonpath.IsSelector(s.column) {
			plain = append(plain, s)
			continue
		}
		if _, raw := s.value.(Expression); raw {
			return nil, fmt.Errorf("%w: raw expressions are not supported in SQLite JSON updates", ErrUnsupported)
		}
		col, path := jsonpath.SplitKey(s.column)
		col = jsonpath.LastSegment(col)
		doc := groups[col]
		if doc == nil {
			doc = map[string]any{}
			groups[col] = doc
		}
		node := doc
		for i, seg := range path {
			if i == len(path)-1 {
				node[seg] = s.value
				break
			}
			next, ok := node[seg].(map[string]any)
			if !ok {
				next = map[string]any{}
				node[seg] = next
			}
			node = next
		}
	}
	cols := make([]string, 0, len(groups))
	for col := range groups {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	for _, col := range cols {
		plain = append(plain, setItem{column: col, value: jsonPatch{groups[col]}})
	}
	return plain, nil
}
