package db

import (
	"fmt"
	"strconv"
	"strings"
)

// selectQuery compiles a SELECT, with a WITH prefix and unions when present. Components follow
// Laravel's order: columns, from, index hint, joins, wheres, groups, havings,
// orders, limit, offset, lock.
func (c *compiler) selectQuery(b *Builder) error {
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
			c.write(" union all ")
		} else {
			c.write(" union ")
		}
		c.write(c.g.UnionOpen())
		if err := c.selectQuery(u.q); err != nil {
			return err
		}
		c.write(")")
	}
	return c.orderLimitOffset(b.unionOrders, b.unionLimit, b.unionOffset)
}

func (c *compiler) selectCore(b *Builder) error {
	if b.table == "" && b.fromSub == nil && b.fromRaw == nil {
		return ErrNoTable
	}
	c.write("select ")
	if b.distinct {
		c.write("distinct ")
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

	c.write(" from ")
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
		c.write(" as ")
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
		c.write(" where ")
		if err := c.clauses(b.wheres); err != nil {
			return err
		}
	}
	if len(b.groups) > 0 {
		c.write(" group by ")
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
		c.write(" having ")
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

// orderLimitOffset writes " order by ... limit n offset m" for a query or a union.
func (c *compiler) orderLimitOffset(orders []orderItem, limit, offset *int) error {
	if len(orders) > 0 {
		c.write(" order by ")
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
		c.write(" limit " + lim)
	}
	if offset != nil {
		c.write(" offset " + strconv.Itoa(*offset))
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
		c.write(" as ")
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
			c.write(" " + cl.boolean + " ")
		case strings.HasSuffix(cl.boolean, "not"):
			c.write("not ")
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
		c.raw(cl.raw)
	case clauseNull:
		c.writeColumn(cl.column)
		if cl.not {
			c.write(" is not null")
		} else {
			c.write(" is null")
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
		c.writeColumn(cl.columns[0])
	case clauseIn, clauseInRaw:
		return c.inClause(cl)
	case clauseBetween:
		c.writeColumn(cl.column)
		c.write(betweenKw(cl.not))
		if err := c.value(cl.values[0]); err != nil {
			return err
		}
		c.write(" and ")
		return c.value(cl.values[1])
	case clauseBetweenColumns:
		c.writeColumn(cl.column)
		c.write(betweenKw(cl.not))
		c.writeColumn(cl.columns[0])
		c.write(" and ")
		c.writeColumn(cl.columns[1])
	case clauseValueBetween:
		if err := c.value(cl.value); err != nil {
			return err
		}
		c.write(betweenKw(cl.not))
		c.writeColumn(cl.columns[0])
		c.write(" and ")
		c.writeColumn(cl.columns[1])
	case clauseNested:
		c.write("(")
		if err := c.clauses(cl.nested); err != nil {
			return err
		}
		c.write(")")
	case clauseExists:
		c.write("exists ")
		return c.subquery(cl.sub)
	case clauseDate:
		c.write(c.g.DatePart(cl.part, c.wrapColumn(cl.column)) + " " + cl.op + " ")
		pre, post := c.g.DateValueWrap()
		c.write(pre)
		if err := c.value(cl.value); err != nil {
			return err
		}
		c.write(post)
	case clauseRowValues:
		cols := make([]string, len(cl.columns))
		for i, col := range cl.columns {
			cols[i] = c.wrapColumn(col)
		}
		c.write("(" + strings.Join(cols, ", ") + ") " + cl.op + " (")
		return c.valueList(cl.values, ")")
	}
	return nil
}

func betweenKw(not bool) string {
	if not {
		return " not between "
	}
	return " between "
}

func (c *compiler) inClause(cl clause) error {
	kw, empty := " in ", "0 = 1"
	if cl.not {
		kw, empty = " not in ", "1 = 1"
	}
	if cl.sub != nil {
		c.writeColumn(cl.column)
		c.write(kw)
		return c.subquery(cl.sub)
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

// subquery writes "(select ...)" sharing this compiler's arg numbering.
func (c *compiler) subquery(q *Builder) error {
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

// value writes a predicate operand: Expression inline, *Builder as subquery,
// anything else as a bind argument.
func (c *compiler) value(v any) error {
	switch x := v.(type) {
	case Expression:
		c.raw(x)
	case *Builder:
		return c.subquery(x)
	default:
		c.param(v)
	}
	return nil
}
