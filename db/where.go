package db

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

// snapshot clones any subquery held by c so later mutation of the caller's
// builder cannot change this query (Laravel compiles subqueries eagerly).
func snapshot(c *clause) {
	if c.sub != nil {
		c.sub = c.sub.Clone()
	}
	if q, ok := c.value.(*Builder); ok {
		c.value = q.Clone()
	}
	for i, v := range c.values {
		if q, ok := v.(*Builder); ok {
			c.values[i] = q.Clone()
		}
	}
}

func (b *Builder) push(dst *[]clause, boolean string, c clause) *Builder {
	snapshot(&c)
	c.boolean = boolean
	*dst = append(*dst, c)
	return b
}

// flatten turns a slice/array (any element type) into []any; a scalar becomes a
// one-element list. []byte is treated as a scalar.
func flatten(v any) []any {
	if vs, ok := v.([]any); ok {
		return vs
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array || rv.Type().Elem().Kind() == reflect.Uint8 {
		return []any{v}
	}
	out := make([]any, rv.Len())
	for i := range out {
		out[i] = rv.Index(i).Interface()
	}
	return out
}

func (b *Builder) inClause(dst *[]clause, boolean, column string, values any, not bool) *Builder {
	if sub, ok := values.(*Builder); ok {
		return b.push(dst, boolean, clause{kind: clauseIn, column: column, sub: sub, not: not})
	}
	return b.push(dst, boolean, clause{kind: clauseIn, column: column, values: flatten(values), not: not})
}

func (b *Builder) inRawClause(dst *[]clause, boolean, column string, values any, not bool) *Builder {
	vs := flatten(values)
	ints := make([]any, len(vs))
	for i, v := range vs {
		n, err := toInt64(v)
		if err != nil {
			return b.fail(fmt.Errorf("%w: %v", ErrInvalidArgs, err))
		}
		ints[i] = n
	}
	return b.push(dst, boolean, clause{kind: clauseInRaw, column: column, values: ints, not: not})
}

func toInt64(v any) (int64, error) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int64(rv.Uint()), nil
	case reflect.String:
		return strconv.ParseInt(rv.String(), 10, 64)
	}
	return 0, fmt.Errorf("not an integer: %T", v)
}

func (b *Builder) group(dst *[]clause, boolean string, fn func(*Builder), having bool) *Builder {
	q := b.NewQuery()
	fn(q)
	if q.err != nil {
		return b.fail(q.err)
	}
	nested := q.wheres
	if having {
		nested = q.havings
	}
	if len(nested) == 0 {
		return b
	}
	return b.push(dst, boolean, clause{kind: clauseNested, nested: nested})
}

func (b *Builder) dateClause(dst *[]clause, boolean, part, column string, args []any) *Builder {
	op, val, err := parseOpValue(column, args)
	if err != nil {
		return b.fail(err)
	}
	if val == nil {
		return b.fail(fmt.Errorf("%w: date comparison needs a non-nil value", ErrInvalidArgs))
	}
	if val, err = dateValue(part, val); err != nil {
		return b.fail(err)
	}
	return b.push(dst, boolean, clause{kind: clauseDate, column: column, part: part, op: op, value: val})
}

var dateLayouts = map[string]string{"date": "2006-01-02", "time": "15:04:05", "day": "02", "month": "01", "year": "2006"}

// dateValue formats time.Time per part, and zero-pads numeric day/month like Laravel.
func dateValue(part string, v any) (any, error) {
	if t, ok := v.(time.Time); ok {
		return t.Format(dateLayouts[part]), nil
	}
	if _, ok := v.(Expression); ok {
		return v, nil
	}
	if part == "day" || part == "month" {
		n, err := toInt64(v)
		if err != nil {
			return nil, fmt.Errorf("%w: %s must be numeric: %v", ErrInvalidArgs, part, err)
		}
		return fmt.Sprintf("%02d", n), nil
	}
	return v, nil
}

func (b *Builder) nullClause(dst *[]clause, boolean, column string, not bool) *Builder {
	return b.push(dst, boolean, clause{kind: clauseNull, column: column, not: not})
}

func (b *Builder) betweenClause(dst *[]clause, boolean, column string, lo, hi any, not bool) *Builder {
	return b.push(dst, boolean, clause{kind: clauseBetween, column: column, values: []any{lo, hi}, not: not})
}

// multi applies a Where-style predicate across columns inside one group.
func (b *Builder) multi(boolean, inner string, columns []string, args []any) *Builder {
	q := b.NewQuery()
	for _, col := range columns {
		q.addClause(&q.wheres, inner, col, args)
	}
	if q.err != nil {
		return b.fail(q.err)
	}
	if len(q.wheres) == 0 {
		return b
	}
	return b.push(&b.wheres, boolean, clause{kind: clauseNested, nested: q.wheres})
}

// ---- NOT ----

// WhereNot adds "and not <predicate>".
func (b *Builder) WhereNot(column string, args ...any) *Builder {
	return b.addClause(&b.wheres, "and not", column, args)
}

// OrWhereNot adds "or not <predicate>".
func (b *Builder) OrWhereNot(column string, args ...any) *Builder {
	return b.addClause(&b.wheres, "or not", column, args)
}

// ---- column comparison ----

// WhereColumn compares two columns: WhereColumn(a, b) means "=", or WhereColumn(a, op, b).
func (b *Builder) WhereColumn(first string, args ...any) *Builder {
	return b.columnClause("and", first, args)
}

// OrWhereColumn is WhereColumn joined with OR.
func (b *Builder) OrWhereColumn(first string, args ...any) *Builder {
	return b.columnClause("or", first, args)
}

func (b *Builder) columnClause(boolean, first string, args []any) *Builder {
	op, val, err := parseOpValue(first, args)
	if err != nil {
		return b.fail(err)
	}
	second, ok := val.(string)
	if !ok {
		return b.fail(fmt.Errorf("%w: second column must be a string", ErrInvalidArgs))
	}
	return b.push(&b.wheres, boolean, clause{kind: clauseColumn, column: first, op: op, columns: []string{second}})
}

// ---- IN ----

// WhereIn adds "col in (...)". values may be any slice, a scalar, or a *Builder subquery.
// An empty list compiles to the always-false "0 = 1".
func (b *Builder) WhereIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "and", column, values, false)
}

// OrWhereIn is WhereIn joined with OR.
func (b *Builder) OrWhereIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "or", column, values, false)
}

// WhereNotIn adds "col not in (...)". An empty list compiles to "1 = 1".
func (b *Builder) WhereNotIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "and", column, values, true)
}

// OrWhereNotIn is WhereNotIn joined with OR.
func (b *Builder) OrWhereNotIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "or", column, values, true)
}

// WhereIntegerInRaw inlines integers (no bind args); non-integers record an error.
// Useful for very large ID lists that would exceed driver parameter limits.
func (b *Builder) WhereIntegerInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "and", column, values, false)
}

// OrWhereIntegerInRaw is WhereIntegerInRaw joined with OR.
func (b *Builder) OrWhereIntegerInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "or", column, values, false)
}

// WhereIntegerNotInRaw is the negated WhereIntegerInRaw.
func (b *Builder) WhereIntegerNotInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "and", column, values, true)
}

// OrWhereIntegerNotInRaw is WhereIntegerNotInRaw joined with OR.
func (b *Builder) OrWhereIntegerNotInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "or", column, values, true)
}

// ---- NULL ----

// WhereNull adds "col is null".
func (b *Builder) WhereNull(column string) *Builder {
	return b.nullClause(&b.wheres, "and", column, false)
}

// OrWhereNull is WhereNull joined with OR.
func (b *Builder) OrWhereNull(column string) *Builder {
	return b.nullClause(&b.wheres, "or", column, false)
}

// WhereNotNull adds "col is not null".
func (b *Builder) WhereNotNull(column string) *Builder {
	return b.nullClause(&b.wheres, "and", column, true)
}

// OrWhereNotNull is WhereNotNull joined with OR.
func (b *Builder) OrWhereNotNull(column string) *Builder {
	return b.nullClause(&b.wheres, "or", column, true)
}

// ---- BETWEEN ----

// WhereBetween adds "col between lo and hi".
func (b *Builder) WhereBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "and", column, lo, hi, false)
}

// OrWhereBetween is WhereBetween joined with OR.
func (b *Builder) OrWhereBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "or", column, lo, hi, false)
}

// WhereNotBetween adds "col not between lo and hi".
func (b *Builder) WhereNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "and", column, lo, hi, true)
}

// OrWhereNotBetween is WhereNotBetween joined with OR.
func (b *Builder) OrWhereNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "or", column, lo, hi, true)
}

func (b *Builder) betweenColumns(boolean, column, c1, c2 string, not bool) *Builder {
	return b.push(&b.wheres, boolean, clause{kind: clauseBetweenColumns, column: column, columns: []string{c1, c2}, not: not})
}

// WhereBetweenColumns adds "col between c1 and c2" comparing against columns.
func (b *Builder) WhereBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("and", column, c1, c2, false)
}

// OrWhereBetweenColumns is WhereBetweenColumns joined with OR.
func (b *Builder) OrWhereBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("or", column, c1, c2, false)
}

// WhereNotBetweenColumns is the negated WhereBetweenColumns.
func (b *Builder) WhereNotBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("and", column, c1, c2, true)
}

// OrWhereNotBetweenColumns is WhereNotBetweenColumns joined with OR.
func (b *Builder) OrWhereNotBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("or", column, c1, c2, true)
}

func (b *Builder) valueBetween(boolean string, value any, c1, c2 string, not bool) *Builder {
	return b.push(&b.wheres, boolean, clause{kind: clauseValueBetween, value: value, columns: []string{c1, c2}, not: not})
}

// WhereValueBetween adds "? between c1 and c2" (a value between two columns).
func (b *Builder) WhereValueBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("and", value, c1, c2, false)
}

// OrWhereValueBetween is WhereValueBetween joined with OR.
func (b *Builder) OrWhereValueBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("or", value, c1, c2, false)
}

// WhereValueNotBetween is the negated WhereValueBetween.
func (b *Builder) WhereValueNotBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("and", value, c1, c2, true)
}

// OrWhereValueNotBetween is WhereValueNotBetween joined with OR.
func (b *Builder) OrWhereValueNotBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("or", value, c1, c2, true)
}

// ---- LIKE / null-safe ----

func (b *Builder) like(boolean, column string, value any, not bool, cs []bool) *Builder {
	return b.push(&b.wheres, boolean, clause{kind: clauseLike, column: column, value: value, not: not, cs: len(cs) > 0 && cs[0]})
}

// WhereLike adds a LIKE predicate, case-insensitive unless caseSensitive[0] is true.
// Postgres: ilike/like. MySQL: like/like binary. SQLite: like/glob (pattern converted).
func (b *Builder) WhereLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("and", column, value, false, caseSensitive)
}

// OrWhereLike is WhereLike joined with OR.
func (b *Builder) OrWhereLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("or", column, value, false, caseSensitive)
}

// WhereNotLike is the negated WhereLike.
func (b *Builder) WhereNotLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("and", column, value, true, caseSensitive)
}

// OrWhereNotLike is WhereNotLike joined with OR.
func (b *Builder) OrWhereNotLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("or", column, value, true, caseSensitive)
}

// WhereNullSafeEquals compares with NULL-safe equality (<=>, is not distinct from, is).
func (b *Builder) WhereNullSafeEquals(column string, value any) *Builder {
	return b.push(&b.wheres, "and", clause{kind: clauseNullSafe, column: column, value: value})
}

// OrWhereNullSafeEquals is WhereNullSafeEquals joined with OR.
func (b *Builder) OrWhereNullSafeEquals(column string, value any) *Builder {
	return b.push(&b.wheres, "or", clause{kind: clauseNullSafe, column: column, value: value})
}

// ---- grouping ----

// WhereGroup wraps the predicates added inside fn in parentheses.
// fn receives a fresh builder; only its Where* calls are used.
func (b *Builder) WhereGroup(fn func(*Builder)) *Builder {
	return b.group(&b.wheres, "and", fn, false)
}

// OrWhereGroup is WhereGroup joined with OR.
func (b *Builder) OrWhereGroup(fn func(*Builder)) *Builder {
	return b.group(&b.wheres, "or", fn, false)
}

// WhereMap adds one grouped "a = ? and b = ?" predicate; keys are sorted for stable SQL.
func (b *Builder) WhereMap(m map[string]any) *Builder { return b.whereMap("and", m) }

// OrWhereMap is WhereMap joined with OR.
func (b *Builder) OrWhereMap(m map[string]any) *Builder { return b.whereMap("or", m) }

func (b *Builder) whereMap(boolean string, m map[string]any) *Builder {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return b.group(&b.wheres, boolean, func(q *Builder) {
		for _, k := range keys {
			q.Where(k, m[k])
		}
	}, false)
}

// ---- EXISTS ----

// WhereExists adds "exists (subquery)". Build sub with NewQuery() or NewBuilder.
func (b *Builder) WhereExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "and", clause{kind: clauseExists, sub: sub})
}

// OrWhereExists is WhereExists joined with OR.
func (b *Builder) OrWhereExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "or", clause{kind: clauseExists, sub: sub})
}

// WhereNotExists adds "not exists (subquery)".
func (b *Builder) WhereNotExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "and not", clause{kind: clauseExists, sub: sub})
}

// OrWhereNotExists is WhereNotExists joined with OR.
func (b *Builder) OrWhereNotExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "or not", clause{kind: clauseExists, sub: sub})
}

// ---- date parts: args are (value) or (operator, value); time.Time is formatted per part ----

// WhereDate compares the date part of a column.
func (b *Builder) WhereDate(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "date", column, args)
}

// OrWhereDate is WhereDate joined with OR.
func (b *Builder) OrWhereDate(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "date", column, args)
}

// WhereTime compares the time part of a column.
func (b *Builder) WhereTime(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "time", column, args)
}

// OrWhereTime is WhereTime joined with OR.
func (b *Builder) OrWhereTime(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "time", column, args)
}

// WhereDay compares the day-of-month of a column (numeric values are zero-padded).
func (b *Builder) WhereDay(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "day", column, args)
}

// OrWhereDay is WhereDay joined with OR.
func (b *Builder) OrWhereDay(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "day", column, args)
}

// WhereMonth compares the month of a column (numeric values are zero-padded).
func (b *Builder) WhereMonth(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "month", column, args)
}

// OrWhereMonth is WhereMonth joined with OR.
func (b *Builder) OrWhereMonth(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "month", column, args)
}

// WhereYear compares the year of a column.
func (b *Builder) WhereYear(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "year", column, args)
}

// OrWhereYear is WhereYear joined with OR.
func (b *Builder) OrWhereYear(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "year", column, args)
}

// ---- all / any / none: args are (value) or (operator, value) ----

// WhereAll requires the predicate to hold for every column: (a = ? and b = ?).
func (b *Builder) WhereAll(columns []string, args ...any) *Builder {
	return b.multi("and", "and", columns, args)
}

// OrWhereAll is WhereAll joined with OR.
func (b *Builder) OrWhereAll(columns []string, args ...any) *Builder {
	return b.multi("or", "and", columns, args)
}

// WhereAny requires the predicate to hold for at least one column: (a = ? or b = ?).
func (b *Builder) WhereAny(columns []string, args ...any) *Builder {
	return b.multi("and", "or", columns, args)
}

// OrWhereAny is WhereAny joined with OR.
func (b *Builder) OrWhereAny(columns []string, args ...any) *Builder {
	return b.multi("or", "or", columns, args)
}

// WhereNone requires the predicate to hold for no column: not (a = ? or b = ?).
func (b *Builder) WhereNone(columns []string, args ...any) *Builder {
	return b.multi("and not", "or", columns, args)
}

// OrWhereNone is WhereNone joined with OR.
func (b *Builder) OrWhereNone(columns []string, args ...any) *Builder {
	return b.multi("or not", "or", columns, args)
}

// ---- row values ----

func (b *Builder) rowValues(boolean string, columns []string, op string, values []any) *Builder {
	op = strings.ToLower(strings.TrimSpace(op))
	if _, ok := operators[op]; !ok {
		return b.fail(fmt.Errorf("%w: %q", ErrInvalidOperator, op))
	}
	if len(columns) == 0 || len(columns) != len(values) {
		return b.fail(fmt.Errorf("%w: row values need equal, non-zero columns and values", ErrInvalidArgs))
	}
	return b.push(&b.wheres, boolean, clause{kind: clauseRowValues, columns: columns, op: op, values: values})
}

// WhereRowValues adds "(a, b) op (?, ?)".
func (b *Builder) WhereRowValues(columns []string, op string, values []any) *Builder {
	return b.rowValues("and", columns, op, values)
}

// OrWhereRowValues is WhereRowValues joined with OR.
func (b *Builder) OrWhereRowValues(columns []string, op string, values []any) *Builder {
	return b.rowValues("or", columns, op, values)
}

// ---- HAVING variants ----

// HavingNull adds "col is null" to HAVING.
func (b *Builder) HavingNull(column string) *Builder {
	return b.nullClause(&b.havings, "and", column, false)
}

// OrHavingNull is HavingNull joined with OR.
func (b *Builder) OrHavingNull(column string) *Builder {
	return b.nullClause(&b.havings, "or", column, false)
}

// HavingNotNull adds "col is not null" to HAVING.
func (b *Builder) HavingNotNull(column string) *Builder {
	return b.nullClause(&b.havings, "and", column, true)
}

// OrHavingNotNull is HavingNotNull joined with OR.
func (b *Builder) OrHavingNotNull(column string) *Builder {
	return b.nullClause(&b.havings, "or", column, true)
}

// HavingBetween adds "col between lo and hi" to HAVING.
func (b *Builder) HavingBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "and", column, lo, hi, false)
}

// OrHavingBetween is HavingBetween joined with OR.
func (b *Builder) OrHavingBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "or", column, lo, hi, false)
}

// HavingNotBetween adds "col not between lo and hi" to HAVING.
func (b *Builder) HavingNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "and", column, lo, hi, true)
}

// OrHavingNotBetween is HavingNotBetween joined with OR.
func (b *Builder) OrHavingNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "or", column, lo, hi, true)
}

// HavingGroup wraps the Having* predicates added inside fn in parentheses.
func (b *Builder) HavingGroup(fn func(*Builder)) *Builder {
	return b.group(&b.havings, "and", fn, true)
}

// OrHavingGroup is HavingGroup joined with OR.
func (b *Builder) OrHavingGroup(fn func(*Builder)) *Builder {
	return b.group(&b.havings, "or", fn, true)
}
