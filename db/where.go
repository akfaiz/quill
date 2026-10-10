package db

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// snapshot clones any subquery held by c so later mutation of the caller's
// builder cannot change this query.
func snapshot(c *clause) {
	if c.x != nil && c.x.sub != nil {
		c.x.sub = c.x.sub.Clone()
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
	c.conn = parseConn(boolean)
	*dst = append(*dst, c)
	return b
}

// flatten turns a slice/array (any element type) into a new []any; a scalar becomes
// a one-element list. []byte is treated as a scalar. The result never shares the
// caller's backing array, so changing the caller's slice later cannot change the query.
func flatten(v any) []any {
	// Common element types skip reflection, which boxes every element on the heap.
	switch vs := v.(type) {
	case []any:
		return slices.Clone(vs)
	case []int:
		return toAny(vs)
	case []int64:
		return toAny(vs)
	case []string:
		return toAny(vs)
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

func toAny[T any](vs []T) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

func (b *Builder) inClause(dst *[]clause, boolean, column string, values any, not bool) *Builder {
	if sub, ok := values.(*Builder); ok {
		if sub == nil {
			return b.fail(errNilSubquery)
		}
		return b.push(dst, boolean, clause{kind: clauseIn, column: column, x: &clauseExt{sub: sub}, not: not})
	}
	return b.push(dst, boolean, clause{kind: clauseIn, column: column, values: flatten(values), not: not})
}

func (b *Builder) inRawClause(dst *[]clause, boolean, column string, values any, not bool) *Builder {
	vs := flatten(values)
	ints := make([]any, len(vs))
	for i, v := range vs {
		n, err := toInt64(v)
		if err != nil {
			return b.fail(fmt.Errorf("%w: %w", ErrInvalidArgs, err))
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
		u := rv.Uint()
		if u > math.MaxInt64 {
			return 0, fmt.Errorf("integer out of range: %d", u)
		}
		return int64(u), nil
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
	return b.push(dst, boolean, clause{kind: clauseNested, x: &clauseExt{nested: nested}})
}

// isolated returns cs wrapped in one parenthesized group when it holds an OR or a
// raw fragment, so a predicate appended after it with AND applies to the whole
// condition: "(a OR b) AND id > ?" instead of "a OR b AND id > ?". A list of
// plain AND clauses is returned unchanged.
func isolated(cs []clause) []clause {
	for _, cl := range cs {
		if cl.kind == clauseRaw || cl.conn == connOr || cl.conn == connOrNot {
			return []clause{{kind: clauseNested, x: &clauseExt{nested: cs}}}
		}
	}
	return cs
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
	return b.push(dst, boolean, clause{kind: clauseDate, column: column, x: &clauseExt{part: part}, op: op, value: val})
}

var dateLayouts = map[string]string{
	"date":  "2006-01-02",
	"time":  "15:04:05",
	"day":   "02",
	"month": "01",
	"year":  "2006",
}

// dateValue formats time.Time per part, and zero-pads numeric day/month.
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
			return nil, fmt.Errorf("%w: %s must be numeric: %w", ErrInvalidArgs, part, err)
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
	return b.push(&b.wheres, boolean, clause{kind: clauseNested, x: &clauseExt{nested: q.wheres}})
}

// ---- NOT ----

// WhereNot adds "AND NOT <predicate>".
//
// Example:
//
//	conn.Table("users").WhereNot("role", "admin")
//	// SELECT * FROM "users" WHERE NOT "role" = $1   args: [admin]
func (b *Builder) WhereNot(column string, args ...any) *Builder {
	return b.addClause(&b.wheres, "and not", column, args)
}

// OrWhereNot adds "OR NOT <predicate>".
//
// Example:
//
//	conn.Table("users").Where("age", ">", 18).OrWhereNot("status", "<", 3)
//	// SELECT * FROM "users" WHERE "age" > $1 OR NOT "status" < $2   args: [18 3]
func (b *Builder) OrWhereNot(column string, args ...any) *Builder {
	return b.addClause(&b.wheres, "or not", column, args)
}

// ---- column comparison ----

// WhereColumn compares two columns: WhereColumn(a, b) means "=", or WhereColumn(a, op, b).
//
// Example:
//
//	conn.Table("users").WhereColumn("updated_at", ">", "created_at")
//	// SELECT * FROM "users" WHERE "updated_at" > "created_at"
func (b *Builder) WhereColumn(first string, args ...any) *Builder {
	return b.columnClause("and", first, args)
}

// OrWhereColumn is WhereColumn joined with OR.
//
// Example:
//
//	conn.Table("users").WhereColumn("first_name", "last_name").OrWhereColumn("a", "<>", "b")
//	// SELECT * FROM "users" WHERE "first_name" = "last_name" OR "a" <> "b"
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
	return b.push(&b.wheres, boolean, clause{kind: clauseColumn, column: first, op: op, column2: second})
}

// ---- IN ----

// WhereIn adds "col IN (...)". values may be any slice, a scalar, or a *Builder subquery.
// An empty list compiles to the always-false "0 = 1".
//
// Example:
//
//	conn.Table("users").WhereIn("id", []int{1, 2, 3})
//	// SELECT * FROM "users" WHERE "id" IN ($1, $2, $3)   args: [1 2 3]
func (b *Builder) WhereIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "and", column, values, false)
}

// OrWhereIn is WhereIn joined with OR.
//
// Example:
//
//	conn.Table("users").WhereIn("role", []string{"ann", "bob"}).OrWhereIn("id", []int{9})
//	// SELECT * FROM "users" WHERE "role" IN ($1, $2) OR "id" IN ($3)   args: [ann bob 9]
func (b *Builder) OrWhereIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "or", column, values, false)
}

// WhereNotIn adds "col NOT IN (...)". An empty list compiles to "1 = 1".
//
// Example:
//
//	conn.Table("users").WhereNotIn("id", []int{1, 2})
//	// SELECT * FROM "users" WHERE "id" NOT IN ($1, $2)   args: [1 2]
func (b *Builder) WhereNotIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "and", column, values, true)
}

// OrWhereNotIn is WhereNotIn joined with OR.
//
// Example:
//
//	conn.Table("users").WhereNotIn("id", []int{1, 2}).OrWhereNotIn("role", []string{"ann"})
//	// SELECT * FROM "users" WHERE "id" NOT IN ($1, $2) OR "role" NOT IN ($3)   args: [1 2 ann]
func (b *Builder) OrWhereNotIn(column string, values any) *Builder {
	return b.inClause(&b.wheres, "or", column, values, true)
}

// WhereIntegerInRaw inlines integers (no bind args); non-integers record an error.
// Useful for very large ID lists that would exceed driver parameter limits.
//
// Example:
//
//	conn.Table("users").WhereIntegerInRaw("id", []int{1, 2, 3})
//	// SELECT * FROM "users" WHERE "id" IN (1, 2, 3)
func (b *Builder) WhereIntegerInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "and", column, values, false)
}

// OrWhereIntegerInRaw is WhereIntegerInRaw joined with OR.
//
// Example:
//
//	conn.Table("users").WhereIntegerInRaw("id", []int{1}).
//		OrWhereIntegerInRaw("id", []int64{7, 8})
//	// SELECT * FROM "users" WHERE "id" IN (1) OR "id" IN (7, 8)
func (b *Builder) OrWhereIntegerInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "or", column, values, false)
}

// WhereIntegerNotInRaw is the negated WhereIntegerInRaw.
//
// Example:
//
//	conn.Table("users").WhereIntegerNotInRaw("id", []int{1, 2})
//	// SELECT * FROM "users" WHERE "id" NOT IN (1, 2)
func (b *Builder) WhereIntegerNotInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "and", column, values, true)
}

// OrWhereIntegerNotInRaw is WhereIntegerNotInRaw joined with OR.
//
// Example:
//
//	conn.Table("users").WhereIntegerNotInRaw("id", []int{1, 2}).
//		OrWhereIntegerNotInRaw("id", []int{3})
//	// SELECT * FROM "users" WHERE "id" NOT IN (1, 2) OR "id" NOT IN (3)
func (b *Builder) OrWhereIntegerNotInRaw(column string, values any) *Builder {
	return b.inRawClause(&b.wheres, "or", column, values, true)
}

// ---- NULL ----

// WhereNull adds "col IS NULL".
//
// Example:
//
//	conn.Table("users").WhereNull("deleted_at")
//	// SELECT * FROM "users" WHERE "deleted_at" IS NULL
func (b *Builder) WhereNull(column string) *Builder {
	return b.nullClause(&b.wheres, "and", column, false)
}

// OrWhereNull is WhereNull joined with OR.
//
// Example:
//
//	conn.Table("users").WhereNull("deleted_at").OrWhereNull("banned_at")
//	// SELECT * FROM "users" WHERE "deleted_at" IS NULL OR "banned_at" IS NULL
func (b *Builder) OrWhereNull(column string) *Builder {
	return b.nullClause(&b.wheres, "or", column, false)
}

// WhereNotNull adds "col IS NOT NULL".
//
// Example:
//
//	conn.Table("users").WhereNotNull("verified_at")
//	// SELECT * FROM "users" WHERE "verified_at" IS NOT NULL
func (b *Builder) WhereNotNull(column string) *Builder {
	return b.nullClause(&b.wheres, "and", column, true)
}

// OrWhereNotNull is WhereNotNull joined with OR.
//
// Example:
//
//	conn.Table("users").WhereNotNull("verified_at").OrWhereNotNull("invited_at")
//	// SELECT * FROM "users" WHERE "verified_at" IS NOT NULL OR "invited_at" IS NOT NULL
func (b *Builder) OrWhereNotNull(column string) *Builder {
	return b.nullClause(&b.wheres, "or", column, true)
}

// ---- BETWEEN ----

// WhereBetween adds "col BETWEEN lo AND hi".
//
// Example:
//
//	conn.Table("orders").WhereBetween("total", 10, 100)
//	// SELECT * FROM "orders" WHERE "total" BETWEEN $1 AND $2   args: [10 100]
func (b *Builder) WhereBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "and", column, lo, hi, false)
}

// OrWhereBetween is WhereBetween joined with OR.
//
// Example:
//
//	conn.Table("orders").Where("status", "paid").OrWhereBetween("total", 500, 900)
//	// SELECT * FROM "orders" WHERE "status" = $1 OR "total" BETWEEN $2 AND $3
//	//     args: [paid 500 900]
func (b *Builder) OrWhereBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "or", column, lo, hi, false)
}

// WhereNotBetween adds "col NOT BETWEEN lo AND hi".
//
// Example:
//
//	conn.Table("orders").WhereNotBetween("total", 10, 100)
//	// SELECT * FROM "orders" WHERE "total" NOT BETWEEN $1 AND $2   args: [10 100]
func (b *Builder) WhereNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "and", column, lo, hi, true)
}

// OrWhereNotBetween is WhereNotBetween joined with OR.
//
// Example:
//
//	conn.Table("orders").WhereNotBetween("total", 10, 100).OrWhereNotBetween("qty", 1, 5)
//	// SELECT * FROM "orders" WHERE "total" NOT BETWEEN $1 AND $2 OR "qty" NOT BETWEEN $3 AND $4
//	//     args: [10 100 1 5]
func (b *Builder) OrWhereNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.wheres, "or", column, lo, hi, true)
}

func (b *Builder) betweenColumns(boolean, column, c1, c2 string, not bool) *Builder {
	return b.push(
		&b.wheres,
		boolean,
		clause{kind: clauseBetweenColumns, column: column, x: &clauseExt{columns: []string{c1, c2}}, not: not},
	)
}

// WhereBetweenColumns adds "col BETWEEN c1 AND c2" comparing against columns.
//
// Example:
//
//	conn.Table("products").WhereBetweenColumns("price", "min_price", "max_price")
//	// SELECT * FROM "products" WHERE "price" BETWEEN "min_price" AND "max_price"
func (b *Builder) WhereBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("and", column, c1, c2, false)
}

// OrWhereBetweenColumns is WhereBetweenColumns joined with OR.
//
// Example:
//
//	conn.Table("products").WhereBetweenColumns("price", "min_price", "max_price").
//		OrWhereBetweenColumns("cost", "low", "high")
//	// SELECT * FROM "products" WHERE "price" BETWEEN "min_price" AND "max_price" OR "cost"
//	//     BETWEEN "low" AND "high"
func (b *Builder) OrWhereBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("or", column, c1, c2, false)
}

// WhereNotBetweenColumns is the negated WhereBetweenColumns.
//
// Example:
//
//	conn.Table("products").WhereNotBetweenColumns("price", "min_price", "max_price")
//	// SELECT * FROM "products" WHERE "price" NOT BETWEEN "min_price" AND "max_price"
func (b *Builder) WhereNotBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("and", column, c1, c2, true)
}

// OrWhereNotBetweenColumns is WhereNotBetweenColumns joined with OR.
//
// Example:
//
//	conn.Table("products").WhereNotBetweenColumns("price", "min_price", "max_price").
//		OrWhereNotBetweenColumns("cost", "low", "high")
//	// SELECT * FROM "products" WHERE "price" NOT BETWEEN "min_price" AND "max_price" OR "cost"
//	//     NOT BETWEEN "low" AND "high"
func (b *Builder) OrWhereNotBetweenColumns(column, c1, c2 string) *Builder {
	return b.betweenColumns("or", column, c1, c2, true)
}

func (b *Builder) valueBetween(boolean string, value any, c1, c2 string, not bool) *Builder {
	return b.push(
		&b.wheres,
		boolean,
		clause{kind: clauseValueBetween, value: value, x: &clauseExt{columns: []string{c1, c2}}, not: not},
	)
}

// WhereValueBetween adds "? BETWEEN c1 AND c2" (a value between two columns).
//
// Example:
//
//	conn.Table("coupons").WhereValueBetween(50, "min_total", "max_total")
//	// SELECT * FROM "coupons" WHERE $1 BETWEEN "min_total" AND "max_total"   args: [50]
func (b *Builder) WhereValueBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("and", value, c1, c2, false)
}

// OrWhereValueBetween is WhereValueBetween joined with OR.
//
// Example:
//
//	conn.Table("coupons").WhereValueBetween(50, "min_total", "max_total").
//		OrWhereValueBetween(7, "a", "b")
//	// SELECT * FROM "coupons" WHERE $1 BETWEEN "min_total" AND "max_total" OR $2 BETWEEN "a" AND
//	//     "b"
//	//     args: [50 7]
func (b *Builder) OrWhereValueBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("or", value, c1, c2, false)
}

// WhereValueNotBetween is the negated WhereValueBetween.
//
// Example:
//
//	conn.Table("coupons").WhereValueNotBetween(50, "min_total", "max_total")
//	// SELECT * FROM "coupons" WHERE $1 NOT BETWEEN "min_total" AND "max_total"   args: [50]
func (b *Builder) WhereValueNotBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("and", value, c1, c2, true)
}

// OrWhereValueNotBetween is WhereValueNotBetween joined with OR.
//
// Example:
//
//	conn.Table("coupons").WhereValueNotBetween(50, "min_total", "max_total").
//		OrWhereValueNotBetween(7, "a", "b")
//	// SELECT * FROM "coupons" WHERE $1 NOT BETWEEN "min_total" AND "max_total" OR $2 NOT BETWEEN
//	//     "a" AND "b"
//	//     args: [50 7]
func (b *Builder) OrWhereValueNotBetween(value any, c1, c2 string) *Builder {
	return b.valueBetween("or", value, c1, c2, true)
}

// ---- LIKE / null-safe ----

func (b *Builder) like(boolean, column string, value any, not bool, cs []bool) *Builder {
	return b.push(
		&b.wheres,
		boolean,
		clause{kind: clauseLike, column: column, value: value, not: not, cs: len(cs) > 0 && cs[0]},
	)
}

// WhereLike adds a LIKE predicate, case-insensitive unless caseSensitive[0] is true.
// Postgres: ILIKE/LIKE. MySQL: LIKE/LIKE BINARY. SQLite: LIKE/GLOB (pattern converted).
//
// Example:
//
//	conn.Table("users").WhereLike("name", "An%")
//	// Postgres: SELECT * FROM "users" WHERE "name"::TEXT ILIKE $1   args: [An%]
//	// MySQL: SELECT * FROM `users` WHERE `name` LIKE ?   args: [An%]
//	// SQLite: SELECT * FROM "users" WHERE "name" LIKE ?   args: [An%]
func (b *Builder) WhereLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("and", column, value, false, caseSensitive)
}

// OrWhereLike is WhereLike joined with OR.
//
// Example:
//
//	conn.Table("users").WhereLike("name", "an%").OrWhereLike("name", "bo%")
//	// SELECT * FROM "users" WHERE "name"::TEXT ILIKE $1 OR "name"::TEXT ILIKE $2
//	//     args: [an% bo%]
func (b *Builder) OrWhereLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("or", column, value, false, caseSensitive)
}

// WhereNotLike is the negated WhereLike.
//
// Example:
//
//	conn.Table("users").WhereNotLike("name", "%test%")
//	// SELECT * FROM "users" WHERE "name"::TEXT NOT ILIKE $1   args: [%test%]
func (b *Builder) WhereNotLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("and", column, value, true, caseSensitive)
}

// OrWhereNotLike is WhereNotLike joined with OR.
//
// Example:
//
//	conn.Table("users").WhereNotLike("name", "%test%").OrWhereNotLike("name", "%demo%", true)
//	// SELECT * FROM "users" WHERE "name"::TEXT NOT ILIKE $1 OR "name"::TEXT NOT LIKE $2
//	//     args: [%test% %demo%]
func (b *Builder) OrWhereNotLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.like("or", column, value, true, caseSensitive)
}

// WhereNullSafeEquals compares with NULL-safe equality (<=>, IS NOT DISTINCT FROM, IS).
//
// Example:
//
//	conn.Table("users").WhereNullSafeEquals("manager_id", 7)
//	// Postgres: SELECT * FROM "users" WHERE "manager_id" IS NOT DISTINCT FROM $1   args: [7]
//	// MySQL: SELECT * FROM `users` WHERE `manager_id` <=> ?   args: [7]
func (b *Builder) WhereNullSafeEquals(column string, value any) *Builder {
	return b.push(&b.wheres, "and", clause{kind: clauseNullSafe, column: column, value: value})
}

// OrWhereNullSafeEquals is WhereNullSafeEquals joined with OR.
//
// Example:
//
//	conn.Table("users").WhereNullSafeEquals("a", 1).OrWhereNullSafeEquals("b", 2)
//	// SELECT * FROM "users" WHERE "a" IS NOT DISTINCT FROM $1 OR "b" IS NOT DISTINCT FROM $2
//	//     args: [1 2]
func (b *Builder) OrWhereNullSafeEquals(column string, value any) *Builder {
	return b.push(&b.wheres, "or", clause{kind: clauseNullSafe, column: column, value: value})
}

// ---- grouping ----

// WhereGroup wraps the predicates added inside fn in parentheses.
// fn receives a fresh builder; only its Where* calls are used.
//
// Example:
//
//	conn.Table("users").Where("active", true).WhereGroup(func(q *db.Builder) {
//		q.Where("role", "admin").OrWhere("role", "editor")
//	})
//	// SELECT * FROM "users" WHERE "active" = $1 AND ("role" = $2 OR "role" = $3)
//	//     args: [true admin editor]
func (b *Builder) WhereGroup(fn func(*Builder)) *Builder {
	return b.group(&b.wheres, "and", fn, false)
}

// OrWhereGroup is WhereGroup joined with OR.
//
// Example:
//
//	conn.Table("users").Where("active", true).OrWhereGroup(func(q *db.Builder) {
//		q.Where("role", "admin").Where("age", ">", 30)
//	})
//	// SELECT * FROM "users" WHERE "active" = $1 OR ("role" = $2 AND "age" > $3)
//	//     args: [true admin 30]
func (b *Builder) OrWhereGroup(fn func(*Builder)) *Builder {
	return b.group(&b.wheres, "or", fn, false)
}

// WhereMap adds one grouped "a = ? and b = ?" predicate; keys are sorted for stable SQL.
//
// Example:
//
//	conn.Table("users").WhereMap(map[string]any{"role": "ann", "active": true})
//	// SELECT * FROM "users" WHERE ("active" = $1 AND "role" = $2)   args: [true ann]
func (b *Builder) WhereMap(m map[string]any) *Builder { return b.whereMap("and", m) }

// OrWhereMap is WhereMap joined with OR.
//
// Example:
//
//	conn.Table("users").WhereMap(map[string]any{"a": 1}).OrWhereMap(map[string]any{"b": 2})
//	// SELECT * FROM "users" WHERE ("a" = $1) OR ("b" = $2)   args: [1 2]
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

// WhereExists adds "EXISTS (subquery)". Build sub with NewQuery() or NewBuilder.
//
// Example:
//
//	posts := conn.Table("posts").Select("1").WhereColumn("posts.user_id", "users.id")
//	conn.Table("users").WhereExists(posts)
//	// SELECT * FROM "users" WHERE EXISTS (SELECT "1" FROM "posts" WHERE "posts"."user_id" =
//	//     "users"."id")
func (b *Builder) WhereExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "and", clause{kind: clauseExists, x: &clauseExt{sub: sub}})
}

// OrWhereExists is WhereExists joined with OR.
//
// Example:
//
//	posts := conn.Table("posts").Select("1").WhereColumn("posts.user_id", "users.id")
//	conn.Table("users").Where("active", true).OrWhereExists(posts)
//	// SELECT * FROM "users" WHERE "active" = $1 OR EXISTS (SELECT "1" FROM "posts" WHERE
//	//     "posts"."user_id" = "users"."id")
//	//     args: [true]
func (b *Builder) OrWhereExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "or", clause{kind: clauseExists, x: &clauseExt{sub: sub}})
}

// WhereNotExists adds "NOT EXISTS (subquery)".
//
// Example:
//
//	posts := conn.Table("posts").Select("1").WhereColumn("posts.user_id", "users.id")
//	conn.Table("users").WhereNotExists(posts)
//	// SELECT * FROM "users" WHERE NOT EXISTS (SELECT "1" FROM "posts" WHERE "posts"."user_id" =
//	//     "users"."id")
func (b *Builder) WhereNotExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "and not", clause{kind: clauseExists, x: &clauseExt{sub: sub}})
}

// OrWhereNotExists is WhereNotExists joined with OR.
//
// Example:
//
//	posts := conn.Table("posts").Select("1").WhereColumn("posts.user_id", "users.id")
//	conn.Table("users").Where("active", true).OrWhereNotExists(posts)
//	// SELECT * FROM "users" WHERE "active" = $1 OR NOT EXISTS (SELECT "1" FROM "posts" WHERE
//	//     "posts"."user_id" = "users"."id")
//	//     args: [true]
func (b *Builder) OrWhereNotExists(sub *Builder) *Builder {
	return b.push(&b.wheres, "or not", clause{kind: clauseExists, x: &clauseExt{sub: sub}})
}

// ---- date parts: args are (value) or (operator, value); time.Time is formatted per part ----

// WhereDate compares the date part of a column.
//
// Example:
//
//	conn.Table("orders").WhereDate("created_at", ">=", "2024-01-15")
//	// Postgres: SELECT * FROM "orders" WHERE "created_at"::DATE >= $1   args: [2024-01-15]
//	// MySQL: SELECT * FROM `orders` WHERE DATE(`created_at`) >= ?   args: [2024-01-15]
//	// SQLite: SELECT * FROM "orders" WHERE STRFTIME('%Y-%m-%d', "created_at") >= CAST(? AS TEXT)
//	//     args: [2024-01-15]
func (b *Builder) WhereDate(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "date", column, args)
}

// OrWhereDate is WhereDate joined with OR.
//
// Example:
//
//	conn.Table("orders").WhereDate("created_at", "2024-01-15").
//		OrWhereDate("created_at", "2024-02-01")
//	// SELECT * FROM "orders" WHERE "created_at"::DATE = $1 OR "created_at"::DATE = $2
//	//     args: [2024-01-15 2024-02-01]
func (b *Builder) OrWhereDate(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "date", column, args)
}

// WhereTime compares the time part of a column.
//
// Example:
//
//	conn.Table("orders").WhereTime("created_at", ">", "10:30:00")
//	// Postgres: SELECT * FROM "orders" WHERE "created_at"::TIME > $1   args: [10:30:00]
//	// MySQL: SELECT * FROM `orders` WHERE TIME(`created_at`) > ?   args: [10:30:00]
func (b *Builder) WhereTime(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "time", column, args)
}

// OrWhereTime is WhereTime joined with OR.
//
// Example:
//
//	conn.Table("orders").WhereTime("created_at", "10:30:00").
//		OrWhereTime("created_at", "<", "08:00:00")
//	// SELECT * FROM "orders" WHERE "created_at"::TIME = $1 OR "created_at"::TIME < $2
//	//     args: [10:30:00 08:00:00]
func (b *Builder) OrWhereTime(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "time", column, args)
}

// WhereDay compares the day-of-month of a column (numeric values are zero-padded).
//
// Example:
//
//	conn.Table("orders").WhereDay("created_at", 15)
//	// Postgres: SELECT * FROM "orders" WHERE EXTRACT(DAY FROM "created_at") = $1   args: [15]
//	// MySQL: SELECT * FROM `orders` WHERE DAY(`created_at`) = ?   args: [15]
func (b *Builder) WhereDay(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "day", column, args)
}

// OrWhereDay is WhereDay joined with OR.
//
// Example:
//
//	conn.Table("orders").WhereDay("created_at", 15).OrWhereDay("created_at", ">", 20)
//	// SELECT * FROM "orders" WHERE EXTRACT(DAY FROM "created_at") = $1 OR EXTRACT(DAY FROM
//	//     "created_at") > $2
//	//     args: [15 20]
func (b *Builder) OrWhereDay(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "day", column, args)
}

// WhereMonth compares the month of a column (numeric values are zero-padded).
//
// Example:
//
//	conn.Table("orders").WhereMonth("created_at", 3)
//	// Postgres: SELECT * FROM "orders" WHERE EXTRACT(MONTH FROM "created_at") = $1   args: [03]
//	// MySQL: SELECT * FROM `orders` WHERE MONTH(`created_at`) = ?   args: [03]
func (b *Builder) WhereMonth(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "month", column, args)
}

// OrWhereMonth is WhereMonth joined with OR.
//
// Example:
//
//	conn.Table("orders").WhereMonth("created_at", ">=", 6).OrWhereMonth("created_at", 1)
//	// SELECT * FROM "orders" WHERE EXTRACT(MONTH FROM "created_at") >= $1 OR EXTRACT(MONTH FROM
//	//     "created_at") = $2
//	//     args: [06 01]
func (b *Builder) OrWhereMonth(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "month", column, args)
}

// WhereYear compares the year of a column.
//
// Example:
//
//	conn.Table("orders").WhereYear("created_at", 2024)
//	// Postgres: SELECT * FROM "orders" WHERE EXTRACT(YEAR FROM "created_at") = $1
//	//     args: [2024]
//	// MySQL: SELECT * FROM `orders` WHERE YEAR(`created_at`) = ?   args: [2024]
func (b *Builder) WhereYear(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "and", "year", column, args)
}

// OrWhereYear is WhereYear joined with OR.
//
// Example:
//
//	conn.Table("orders").WhereYear("created_at", ">", 2020).OrWhereYear("created_at", 1999)
//	// SELECT * FROM "orders" WHERE EXTRACT(YEAR FROM "created_at") > $1 OR EXTRACT(YEAR FROM
//	//     "created_at") = $2
//	//     args: [2020 1999]
func (b *Builder) OrWhereYear(column string, args ...any) *Builder {
	return b.dateClause(&b.wheres, "or", "year", column, args)
}

// ---- all / any / none: args are (value) or (operator, value) ----

// WhereAll requires the predicate to hold for every column: (a = ? and b = ?).
//
// Example:
//
//	conn.Table("users").WhereAll([]string{"first_name", "last_name"}, "like", "an%")
//	// SELECT * FROM "users" WHERE ("first_name"::TEXT LIKE $1 AND "last_name"::TEXT LIKE $2)
//	//     args: [an% an%]
func (b *Builder) WhereAll(columns []string, args ...any) *Builder {
	return b.multi("and", "and", columns, args)
}

// OrWhereAll is WhereAll joined with OR.
//
// Example:
//
//	conn.Table("users").WhereAll([]string{"a", "b"}, 1).OrWhereAll([]string{"c", "d"}, ">", 5)
//	// SELECT * FROM "users" WHERE ("a" = $1 AND "b" = $2) OR ("c" > $3 AND "d" > $4)
//	//     args: [1 1 5 5]
func (b *Builder) OrWhereAll(columns []string, args ...any) *Builder {
	return b.multi("or", "and", columns, args)
}

// WhereAny requires the predicate to hold for at least one column: (a = ? or b = ?).
//
// Example:
//
//	conn.Table("users").WhereAny([]string{"first_name", "last_name"}, "ann")
//	// SELECT * FROM "users" WHERE ("first_name" = $1 OR "last_name" = $2)   args: [ann ann]
func (b *Builder) WhereAny(columns []string, args ...any) *Builder {
	return b.multi("and", "or", columns, args)
}

// OrWhereAny is WhereAny joined with OR.
//
// Example:
//
//	conn.Table("users").Where("active", true).OrWhereAny([]string{"a", "b"}, ">", 5)
//	// SELECT * FROM "users" WHERE "active" = $1 OR ("a" > $2 OR "b" > $3)   args: [true 5 5]
func (b *Builder) OrWhereAny(columns []string, args ...any) *Builder {
	return b.multi("or", "or", columns, args)
}

// WhereNone requires the predicate to hold for no column: not (a = ? or b = ?).
//
// Example:
//
//	conn.Table("users").WhereNone([]string{"first_name", "last_name"}, "like", "%test%")
//	// SELECT * FROM "users" WHERE NOT ("first_name"::TEXT LIKE $1 OR "last_name"::TEXT LIKE $2)
//	//     args: [%test% %test%]
func (b *Builder) WhereNone(columns []string, args ...any) *Builder {
	return b.multi("and not", "or", columns, args)
}

// OrWhereNone is WhereNone joined with OR.
//
// Example:
//
//	conn.Table("users").Where("active", true).OrWhereNone([]string{"a", "b"}, 0)
//	// SELECT * FROM "users" WHERE "active" = $1 OR NOT ("a" = $2 OR "b" = $3)
//	//     args: [true 0 0]
func (b *Builder) OrWhereNone(columns []string, args ...any) *Builder {
	return b.multi("or not", "or", columns, args)
}

// ---- row values ----

func (b *Builder) rowValues(boolean string, columns []string, op string, values []any) *Builder {
	op = strings.ToLower(strings.TrimSpace(op))
	canon, ok := operators[op]
	if !ok {
		return b.fail(fmt.Errorf("%w: %q", ErrInvalidOperator, op))
	}
	op = canon
	if len(columns) == 0 || len(columns) != len(values) {
		return b.fail(fmt.Errorf("%w: row values need equal, non-zero columns and values", ErrInvalidArgs))
	}
	return b.push(
		&b.wheres,
		boolean,
		clause{
			kind:   clauseRowValues,
			x:      &clauseExt{columns: slices.Clone(columns)},
			op:     op,
			values: slices.Clone(values),
		},
	)
}

// WhereRowValues adds "(a, b) op (?, ?)".
//
// Example:
//
//	conn.Table("events").
//		WhereRowValues([]string{"created_at", "id"}, ">", []any{"2024-01-15", 42})
//	// SELECT * FROM "events" WHERE ("created_at", "id") > ($1, $2)   args: [2024-01-15 42]
func (b *Builder) WhereRowValues(columns []string, op string, values []any) *Builder {
	return b.rowValues("and", columns, op, values)
}

// OrWhereRowValues is WhereRowValues joined with OR.
//
// Example:
//
//	conn.Table("events").Where("kind", "x").
//		OrWhereRowValues([]string{"a", "b"}, "=", []any{1, 2})
//	// SELECT * FROM "events" WHERE "kind" = $1 OR ("a", "b") = ($2, $3)   args: [x 1 2]
func (b *Builder) OrWhereRowValues(columns []string, op string, values []any) *Builder {
	return b.rowValues("or", columns, op, values)
}

// ---- HAVING variants ----

// HavingNull adds "col IS NULL" to HAVING.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingNull("spent")
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent" IS
//	//     NULL
func (b *Builder) HavingNull(column string) *Builder {
	return b.nullClause(&b.havings, "and", column, false)
}

// OrHavingNull is HavingNull joined with OR.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingNull("spent").OrHavingNull("user_id")
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent" IS
//	//     NULL OR "user_id" IS NULL
func (b *Builder) OrHavingNull(column string) *Builder {
	return b.nullClause(&b.havings, "or", column, false)
}

// HavingNotNull adds "col IS NOT NULL" to HAVING.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingNotNull("spent")
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent" IS
//	//     NOT NULL
func (b *Builder) HavingNotNull(column string) *Builder {
	return b.nullClause(&b.havings, "and", column, true)
}

// OrHavingNotNull is HavingNotNull joined with OR.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingNotNull("spent").OrHavingNotNull("user_id")
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent" IS
//	//     NOT NULL OR "user_id" IS NOT NULL
func (b *Builder) OrHavingNotNull(column string) *Builder {
	return b.nullClause(&b.havings, "or", column, true)
}

// HavingBetween adds "col BETWEEN lo AND hi" to HAVING.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingBetween("spent", 100, 500)
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent"
//	//     BETWEEN $1 AND $2
//	//     args: [100 500]
func (b *Builder) HavingBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "and", column, lo, hi, false)
}

// OrHavingBetween is HavingBetween joined with OR.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingBetween("spent", 100, 500).OrHavingBetween("user_id", 1, 3)
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent"
//	//     BETWEEN $1 AND $2 OR "user_id" BETWEEN $3 AND $4
//	//     args: [100 500 1 3]
func (b *Builder) OrHavingBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "or", column, lo, hi, false)
}

// HavingNotBetween adds "col NOT BETWEEN lo AND hi" to HAVING.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingNotBetween("spent", 100, 500)
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent" NOT
//	//     BETWEEN $1 AND $2
//	//     args: [100 500]
func (b *Builder) HavingNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "and", column, lo, hi, true)
}

// OrHavingNotBetween is HavingNotBetween joined with OR.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		HavingNotBetween("spent", 100, 500).OrHavingNotBetween("user_id", 1, 3)
//	// SELECT "user_id", sum(total) as spent FROM "orders" GROUP BY "user_id" HAVING "spent" NOT
//	//     BETWEEN $1 AND $2 OR "user_id" NOT BETWEEN $3 AND $4
//	//     args: [100 500 1 3]
func (b *Builder) OrHavingNotBetween(column string, lo, hi any) *Builder {
	return b.betweenClause(&b.havings, "or", column, lo, hi, true)
}

// HavingGroup wraps the Having* predicates added inside fn in parentheses.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		SelectRaw("count(*) as n").HavingGroup(func(q *db.Builder) {
//		q.Having("spent", ">", 100).OrHaving("n", ">", 5)
//	})
//	// SELECT "user_id", sum(total) as spent, count(*) as n FROM "orders" GROUP BY "user_id"
//	//     HAVING ("spent" > $1 OR "n" > $2)
//	//     args: [100 5]
func (b *Builder) HavingGroup(fn func(*Builder)) *Builder {
	return b.group(&b.havings, "and", fn, true)
}

// OrHavingGroup is HavingGroup joined with OR.
//
// Example:
//
//	conn.Table("orders").Select("user_id").SelectRaw("sum(total) as spent").GroupBy("user_id").
//		SelectRaw("count(*) as n").Having("n", ">", 1).OrHavingGroup(func(q *db.Builder) {
//		q.Having("spent", ">", 100).Having("n", "<", 3)
//	})
//	// SELECT "user_id", sum(total) as spent, count(*) as n FROM "orders" GROUP BY "user_id"
//	//     HAVING "n" > $1 OR ("spent" > $2 AND "n" < $3)
//	//     args: [1 100 3]
func (b *Builder) OrHavingGroup(fn func(*Builder)) *Builder {
	return b.group(&b.havings, "or", fn, true)
}
