package db

import "fmt"

type joinClause struct {
	typ     string // inner, left, right, cross, straight_join
	table   string
	sub     *Builder // subquery source; alias is required
	alias   string
	lateral bool
	on      []clause
}

func cloneJoins(js []joinClause) []joinClause {
	if js == nil {
		return nil
	}
	out := make([]joinClause, len(js))
	copy(out, js)
	for i := range out {
		out[i].on = cloneClauses(out[i].on)
		if out[i].sub != nil {
			out[i].sub = out[i].sub.Clone()
		}
	}
	return out
}

// JoinClause builds the ON conditions of one join. Column-to-column
// comparisons use On/OrOn; value comparisons use Where/OrWhere.
type JoinClause struct{ q *Builder }

// On adds "first = second" or "first op second" (column to column).
func (j *JoinClause) On(first string, args ...any) *JoinClause {
	j.q.columnClause("and", first, args)
	return j
}

// OrOn is On joined with OR.
func (j *JoinClause) OrOn(first string, args ...any) *JoinClause {
	j.q.columnClause("or", first, args)
	return j
}

// OnGroup wraps the conditions added inside fn in parentheses.
func (j *JoinClause) OnGroup(fn func(*JoinClause)) *JoinClause {
	j.q.WhereGroup(func(q *Builder) { fn(&JoinClause{q}) })
	return j
}

// OrOnGroup is OnGroup joined with OR.
func (j *JoinClause) OrOnGroup(fn func(*JoinClause)) *JoinClause {
	j.q.OrWhereGroup(func(q *Builder) { fn(&JoinClause{q}) })
	return j
}

// Where adds a bound-value condition: Where(col, value) or Where(col, op, value).
func (j *JoinClause) Where(column string, args ...any) *JoinClause {
	j.q.Where(column, args...)
	return j
}

// OrWhere is Where joined with OR.
func (j *JoinClause) OrWhere(column string, args ...any) *JoinClause {
	j.q.OrWhere(column, args...)
	return j
}

// WhereIn adds "col in (...)".
func (j *JoinClause) WhereIn(column string, values any) *JoinClause {
	j.q.WhereIn(column, values)
	return j
}

// WhereNotIn adds "col not in (...)".
func (j *JoinClause) WhereNotIn(column string, values any) *JoinClause {
	j.q.WhereNotIn(column, values)
	return j
}

// WhereNull adds "col is null".
func (j *JoinClause) WhereNull(column string) *JoinClause {
	j.q.WhereNull(column)
	return j
}

// WhereNotNull adds "col is not null".
func (j *JoinClause) WhereNotNull(column string) *JoinClause {
	j.q.WhereNotNull(column)
	return j
}

// WhereRaw adds a raw condition (caller-trusted SQL).
func (j *JoinClause) WhereRaw(sql string, args ...any) *JoinClause {
	j.q.WhereRaw(sql, args...)
	return j
}

// OrWhereRaw is WhereRaw joined with OR.
func (j *JoinClause) OrWhereRaw(sql string, args ...any) *JoinClause {
	j.q.OrWhereRaw(sql, args...)
	return j
}

// addJoin resolves the ON conditions from fn, or from (first, args) as a column
// comparison (where=false) or a value comparison (where=true).
func (b *Builder) addJoin(j joinClause, first string, args []any, where bool, fn func(*JoinClause)) *Builder {
	q := b.NewQuery()
	switch {
	case fn != nil:
		fn(&JoinClause{q})
	case first != "" || len(args) > 0:
		if where {
			q.addClause(&q.wheres, "and", first, args)
		} else {
			q.columnClause("and", first, args)
		}
	}
	if q.err != nil {
		return b.fail(q.err)
	}
	j.on = q.wheres
	b.joins = append(b.joins, j)
	return b
}

func tbl(typ, table string) joinClause { return joinClause{typ: typ, table: table} }

func subJoin(typ string, sub *Builder, as string) joinClause {
	if sub != nil {
		sub = sub.Clone() // snapshot, see snapshot() in where.go
	}
	return joinClause{typ: typ, sub: sub, alias: as}
}

// Join adds "inner join table on first = second" (or "first op second").
func (b *Builder) Join(table, first string, args ...any) *Builder {
	return b.addJoin(tbl("inner", table), first, args, false, nil)
}

// LeftJoin adds a left join.
func (b *Builder) LeftJoin(table, first string, args ...any) *Builder {
	return b.addJoin(tbl("left", table), first, args, false, nil)
}

// RightJoin adds a right join.
func (b *Builder) RightJoin(table, first string, args ...any) *Builder {
	return b.addJoin(tbl("right", table), first, args, false, nil)
}

// CrossJoin adds "cross join table".
func (b *Builder) CrossJoin(table string) *Builder {
	return b.addJoin(tbl("cross", table), "", nil, false, nil)
}

// StraightJoin adds a MySQL "straight_join"; other dialects error at compile time.
func (b *Builder) StraightJoin(table, first string, args ...any) *Builder {
	return b.addJoin(tbl("straight_join", table), first, args, false, nil)
}

// JoinFunc adds an inner join with a complex ON clause built in fn.
func (b *Builder) JoinFunc(table string, fn func(*JoinClause)) *Builder {
	return b.addJoin(tbl("inner", table), "", nil, false, fn)
}

// LeftJoinFunc adds a left join with a complex ON clause built in fn.
func (b *Builder) LeftJoinFunc(table string, fn func(*JoinClause)) *Builder {
	return b.addJoin(tbl("left", table), "", nil, false, fn)
}

// RightJoinFunc adds a right join with a complex ON clause built in fn.
func (b *Builder) RightJoinFunc(table string, fn func(*JoinClause)) *Builder {
	return b.addJoin(tbl("right", table), "", nil, false, fn)
}

// JoinWhere joins on a bound value: "inner join t on col = ?".
func (b *Builder) JoinWhere(table, column string, args ...any) *Builder {
	return b.addJoin(tbl("inner", table), column, args, true, nil)
}

// LeftJoinWhere is JoinWhere as a left join.
func (b *Builder) LeftJoinWhere(table, column string, args ...any) *Builder {
	return b.addJoin(tbl("left", table), column, args, true, nil)
}

// RightJoinWhere is JoinWhere as a right join.
func (b *Builder) RightJoinWhere(table, column string, args ...any) *Builder {
	return b.addJoin(tbl("right", table), column, args, true, nil)
}

// JoinSub joins a subquery: "inner join (select ...) as alias on ...".
func (b *Builder) JoinSub(sub *Builder, as, first string, args ...any) *Builder {
	return b.addJoin(subJoin("inner", sub, as), first, args, false, nil)
}

// LeftJoinSub joins a subquery with a left join.
func (b *Builder) LeftJoinSub(sub *Builder, as, first string, args ...any) *Builder {
	return b.addJoin(subJoin("left", sub, as), first, args, false, nil)
}

// RightJoinSub joins a subquery with a right join.
func (b *Builder) RightJoinSub(sub *Builder, as, first string, args ...any) *Builder {
	return b.addJoin(subJoin("right", sub, as), first, args, false, nil)
}

// CrossJoinSub cross joins a subquery.
func (b *Builder) CrossJoinSub(sub *Builder, as string) *Builder {
	return b.addJoin(subJoin("cross", sub, as), "", nil, false, nil)
}

// JoinSubFunc joins a subquery with a complex ON clause built in fn.
func (b *Builder) JoinSubFunc(sub *Builder, as string, fn func(*JoinClause)) *Builder {
	return b.addJoin(subJoin("inner", sub, as), "", nil, false, fn)
}

// LeftJoinSubFunc is JoinSubFunc as a left join.
func (b *Builder) LeftJoinSubFunc(sub *Builder, as string, fn func(*JoinClause)) *Builder {
	return b.addJoin(subJoin("left", sub, as), "", nil, false, fn)
}

// RightJoinSubFunc is JoinSubFunc as a right join.
func (b *Builder) RightJoinSubFunc(sub *Builder, as string, fn func(*JoinClause)) *Builder {
	return b.addJoin(subJoin("right", sub, as), "", nil, false, fn)
}

// JoinLateral adds "inner join lateral (subquery) as alias on true" (Postgres, MySQL >= 8.0.14).
func (b *Builder) JoinLateral(sub *Builder, as string) *Builder {
	j := subJoin("inner", sub, as)
	j.lateral = true
	return b.addJoin(j, "", nil, false, nil)
}

// LeftJoinLateral is JoinLateral as a left join.
func (b *Builder) LeftJoinLateral(sub *Builder, as string) *Builder {
	j := subJoin("left", sub, as)
	j.lateral = true
	return b.addJoin(j, "", nil, false, nil)
}

func (c *compiler) join(j joinClause) error {
	if j.typ == "straight_join" && !c.g.SupportsStraightJoin() {
		return fmt.Errorf("%w: straight_join", ErrUnsupported)
	}
	if j.lateral && !c.g.SupportsLateralJoin() {
		return fmt.Errorf("%w: lateral join", ErrUnsupported)
	}
	if j.typ == "straight_join" {
		c.write("straight_join ")
	} else {
		c.write(j.typ + " join ")
	}
	if j.lateral {
		c.write("lateral ")
	}
	if j.sub != nil {
		if j.alias == "" {
			return fmt.Errorf("%w: subquery join needs an alias", ErrInvalidArgs)
		}
		if err := c.subquery(j.sub); err != nil {
			return err
		}
		c.write(" as " + c.wrapTable(j.alias))
	} else {
		c.write(c.wrapTable(j.table))
	}
	if j.lateral {
		c.write(" on true")
		return nil
	}
	if len(j.on) == 0 {
		if j.typ == "cross" {
			return nil
		}
		return ErrJoinNeedsOn
	}
	c.write(" on ")
	return c.clauses(j.on)
}

// OrWhereIn is WhereIn joined with OR.
func (j *JoinClause) OrWhereIn(column string, values any) *JoinClause {
	j.q.OrWhereIn(column, values)
	return j
}

// OrWhereNotIn is WhereNotIn joined with OR.
func (j *JoinClause) OrWhereNotIn(column string, values any) *JoinClause {
	j.q.OrWhereNotIn(column, values)
	return j
}

// OrWhereNull is WhereNull joined with OR.
func (j *JoinClause) OrWhereNull(column string) *JoinClause {
	j.q.OrWhereNull(column)
	return j
}

// OrWhereNotNull is WhereNotNull joined with OR.
func (j *JoinClause) OrWhereNotNull(column string) *JoinClause {
	j.q.OrWhereNotNull(column)
	return j
}
