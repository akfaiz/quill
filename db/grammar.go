package db

import (
	"fmt"
	"strconv"
	"strings"
)

// selectQuery compiles a SELECT, with a WITH prefix and unions when present. Components follow
// the usual order: columns, from, index hint, joins, wheres, groups, havings,
// orders, limit, offset, lock.
func (c *compiler) selectQuery(b *query) error {
	if err := c.with(b); err != nil {
		return err
	}
	if len(b.unions) == 0 {
		return c.selectCore(b)
	}
	c.write(c.g.UnionOpen())
	if err := c.selectCore(b); err != nil {
		return err
	}
	c.write(")")
	for _, u := range b.unions {
		if u.q.err != nil {
			return u.q.err
		}
		if u.all {
			c.write(" UNION ALL ")
		} else {
			c.write(" UNION ")
		}
		c.write(c.g.UnionOpen())
		if err := c.selectQuery(u.q); err != nil {
			return err
		}
		c.write(")")
	}
	return c.orderLimitOffset(b.unionOrders, b.unionLimit, b.unionOffset)
}

func (c *compiler) selectCore(b *query) error {
	if b.table == "" && b.fromSub == nil && b.fromRaw == nil {
		return ErrNoTable
	}
	c.write("SELECT ")
	if b.distinct {
		c.write("DISTINCT ")
	}
	if len(b.columns) == 0 {
		c.write("*")
	}
	for i, it := range b.columns {
		if i > 0 {
			c.write(", ")
		}
		if err := c.item(it); err != nil {
			return err
		}
	}

	c.write(" FROM ")
	switch {
	case b.fromRaw != nil:
		c.raw(*b.fromRaw)
	case b.fromSub != nil:
		if b.fromAlias == "" {
			return fmt.Errorf("%w: FromSub needs an alias", ErrInvalidArgs)
		}
		if err := c.subquery(b.fromSub); err != nil {
			return err
		}
		c.write(" AS ")
		c.writeTable(b.fromAlias)
	default:
		c.writeTable(b.table)
	}

	if b.hint != nil {
		h, err := c.g.IndexHint(b.hint.kind, b.hint.index)
		if err != nil {
			return err
		}
		if h != "" {
			c.write(" " + h)
		}
	}

	for _, j := range b.joins {
		c.write(" ")
		if err := c.join(j); err != nil {
			return err
		}
	}

	if len(b.wheres) > 0 {
		c.write(" WHERE ")
		if err := c.clauses(b.wheres); err != nil {
			return err
		}
	}
	if len(b.groups) > 0 {
		c.write(" GROUP BY ")
		for i, it := range b.groups {
			if i > 0 {
				c.write(", ")
			}
			if err := c.item(it); err != nil {
				return err
			}
		}
	}
	if len(b.havings) > 0 {
		c.write(" HAVING ")
		if err := c.clauses(b.havings); err != nil {
			return err
		}
	}
	if err := c.orderLimitOffset(b.orders, b.limit, b.offset); err != nil {
		return err
	}
	if l := c.g.LockSQL(b.lock, b.lockRaw); l != "" {
		c.write(" " + l)
	}
	return nil
}

// orderLimitOffset writes " ORDER BY ... LIMIT n OFFSET m" for a query or a union.
func (c *compiler) orderLimitOffset(orders []orderItem, limit, offset *int) error {
	if len(orders) > 0 {
		c.write(" ORDER BY ")
		for i, o := range orders {
			if i > 0 {
				c.write(", ")
			}
			if o.raw != nil {
				c.raw(*o.raw)
				continue
			}
			c.writeColumn(o.column)
			c.write(" ")
			c.write(o.dir)
		}
	}
	lim := ""
	if limit != nil {
		lim = strconv.Itoa(*limit)
	} else if offset != nil {
		lim = c.g.UnboundedLimit()
	}
	if lim != "" {
		c.write(" LIMIT " + lim)
	}
	if offset != nil {
		c.write(" OFFSET " + strconv.Itoa(*offset))
	}
	return nil
}

func (c *compiler) item(it selectItem) error {
	switch {
	case it.raw != nil:
		c.raw(*it.raw)
	case it.sub != nil:
		if err := c.subquery(it.sub); err != nil {
			return err
		}
		c.write(" AS ")
		c.writeColumn(it.alias)
	default:
		c.writeColumn(it.column)
	}
	return nil
}

func (c *compiler) clauses(cs []clause) error {
	for i, cl := range cs {
		switch {
		case i > 0:
			c.write(" ")
			c.write(cl.conn.String())
			c.write(" ")
		case cl.conn.negated():
			c.write("NOT ")
		}
		if err := c.clause(cl); err != nil {
			return err
		}
	}
	return nil
}

func (c *compiler) clause(cl clause) error {
	switch cl.kind {
	case clauseRaw:
		c.raw(cl.rawExpr())
	case clauseNull:
		c.writeColumn(cl.column)
		if cl.not {
			c.write(" IS NOT NULL")
		} else {
			c.write(" IS NULL")
		}
	case clauseBasic:
		c.writeColumn(cl.column)
		c.write(c.g.LikeCast(cl.op))
		c.write(" ")
		c.write(cl.op)
		c.write(" ")
		return c.value(cl.value)
	case clauseLike:
		op := c.g.LikeOp(cl.cs, cl.not)
		c.writeColumn(cl.column)
		c.write(c.g.LikeCast(op))
		c.write(" ")
		c.write(op)
		c.write(" ")
		if s, ok := cl.value.(string); ok {
			cl.value = c.g.LikeValue(s, cl.cs)
		}
		return c.value(cl.value)
	case clauseJSONBool, clauseJSONContains, clauseJSONOverlaps, clauseJSONContainsKey, clauseJSONLength:
		return c.jsonClause(cl)
	case clauseNullSafe:
		c.writeColumn(cl.column)
		c.write(" ")
		c.write(c.g.NullSafeOp())
		c.write(" ")
		return c.value(cl.value)
	case clauseColumn:
		c.writeColumn(cl.column)
		c.write(" ")
		c.write(cl.op)
		c.write(" ")
		c.writeColumn(cl.column2)
	case clauseIn, clauseInRaw:
		return c.inClause(cl)
	case clauseBetween:
		c.writeColumn(cl.column)
		c.write(betweenKw(cl.not))
		if err := c.value(cl.values[0]); err != nil {
			return err
		}
		c.write(" AND ")
		return c.value(cl.values[1])
	case clauseBetweenColumns:
		c.writeColumn(cl.column)
		c.write(betweenKw(cl.not))
		c.writeColumn(cl.cols()[0])
		c.write(" AND ")
		c.writeColumn(cl.cols()[1])
	case clauseValueBetween:
		if err := c.value(cl.value); err != nil {
			return err
		}
		c.write(betweenKw(cl.not))
		c.writeColumn(cl.cols()[0])
		c.write(" AND ")
		c.writeColumn(cl.cols()[1])
	case clauseNested:
		c.write("(")
		if err := c.clauses(cl.kids()); err != nil {
			return err
		}
		c.write(")")
	case clauseExists:
		c.write("EXISTS ")
		return c.subquery(cl.subquery())
	case clauseDate:
		c.write(c.g.DatePart(cl.datePart(), c.wrapColumn(cl.column)) + " " + cl.op + " ")
		pre, post := c.g.DateValueWrap()
		c.write(pre)
		if err := c.value(cl.value); err != nil {
			return err
		}
		c.write(post)
	case clauseRowValues:
		rowCols := cl.cols()
		cols := make([]string, len(rowCols))
		for i, col := range rowCols {
			cols[i] = c.wrapColumn(col)
		}
		c.write("(" + strings.Join(cols, ", ") + ") " + cl.op + " (")
		return c.valueList(cl.values, ")")
	}
	return nil
}

func betweenKw(not bool) string {
	if not {
		return " NOT BETWEEN "
	}
	return " BETWEEN "
}

func (c *compiler) inClause(cl clause) error {
	kw, empty := " IN ", "0 = 1"
	if cl.not {
		kw, empty = " NOT IN ", "1 = 1"
	}
	if sub := cl.subquery(); sub != nil {
		c.writeColumn(cl.column)
		c.write(kw)
		return c.subquery(cl.subquery())
	}
	if len(cl.values) == 0 {
		c.write(empty)
		return nil
	}
	c.writeColumn(cl.column)
	c.write(kw)
	c.write("(")
	if cl.kind == clauseInRaw {
		for i, v := range cl.values {
			if i > 0 {
				c.write(", ")
			}
			c.write(strconv.FormatInt(v.(int64), 10))
		}
		c.write(")")
		return nil
	}
	return c.valueList(cl.values, ")")
}

// valueList writes comma-separated operands followed by closer.
func (c *compiler) valueList(vs []any, closer string) error {
	for i, v := range vs {
		if i > 0 {
			c.write(", ")
		}
		if err := c.value(v); err != nil {
			return err
		}
	}
	c.write(closer)
	return nil
}

// subquery writes "(SELECT ...)" sharing this compiler's arg numbering.
func (c *compiler) subquery(q *query) error {
	if q == nil {
		return errNilSubquery
	}
	if q.err != nil {
		return q.err
	}
	c.write("(")
	if err := c.selectQuery(q); err != nil {
		return err
	}
	c.write(")")
	return nil
}

// value writes a predicate operand: Expression inline, *query as subquery,
// anything else as a bind argument.
func (c *compiler) value(v any) error {
	switch x := v.(type) {
	case Expression:
		c.raw(x)
	case *query:
		return c.subquery(x)
	case *SelectBuilder:
		return c.subquery(sq(x))
	default:
		c.param(v)
	}
	return nil
}
