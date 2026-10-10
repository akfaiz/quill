package db

import (
	"fmt"
	"slices"
	"strings"
)

// cteClause is one "name (cols) as (body)" entry of a WITH clause.
type cteClause struct {
	name      string
	cols      []string
	q         *Builder
	raw       *Expression
	recursive bool
	mat       int8 // 0 default, 1 materialized, -1 not materialized
}

// With adds a common table expression: "with name (cols) as (q)". The name is
// a table reference, so the table prefix applies to it as it does to From.
// The subquery is snapshotted: later changes to q do not affect this query.
//
// Example:
//
//	active := conn.Table("users").Where("active", true)
//	conn.Table("active_users").With("active_users", active)
//	// WITH "active_users" AS (SELECT * FROM "users" WHERE "active" = $1) SELECT * FROM
//	//     "active_users"
//	// args: [true]
func (b *Builder) With(name string, q *Builder, cols ...string) *Builder {
	return b.addCTE(cteClause{name: name, q: q, cols: cols})
}

// WithRecursive adds a recursive common table expression and switches the
// clause to "with recursive". q is the anchor with the recursive member added
// by UnionAll (or Union); the members are emitted without parentheses.
//
// Example:
//
//	anchor := conn.Table("categories").Where("parent_id", nil)
//	member := conn.Table("categories as c").Join("tree as t", "c.parent_id", "t.id")
//	conn.Table("tree").WithRecursive("tree", anchor.UnionAll(member))
//	// WITH RECURSIVE "tree" AS (SELECT * FROM "categories" WHERE "parent_id" IS NULL UNION ALL
//	//     SELECT * FROM "categories" AS "c" INNER JOIN "tree" AS "t" ON "c"."parent_id" =
//	//     "t"."id") SELECT * FROM "tree"
func (b *Builder) WithRecursive(name string, q *Builder, cols ...string) *Builder {
	return b.addCTE(cteClause{name: name, q: q, cols: cols, recursive: true})
}

// WithRaw adds a common table expression whose body is a raw SQL fragment
// (caller-trusted SQL), e.g. db.Raw("values (?), (?)", 1, 2).
//
// Example:
//
//	conn.Table("ids").WithRaw("ids", db.Raw("values (?), (?)", 1, 2), "id")
//	// WITH "ids" ("id") AS (values ($1), ($2)) SELECT * FROM "ids"
//	// args: [1 2]
func (b *Builder) WithRaw(name string, e Expression, cols ...string) *Builder {
	return b.addCTE(cteClause{name: name, raw: &e, cols: cols})
}

// WithMaterialized adds "name as materialized (q)". PostgreSQL only; other
// dialects return ErrUnsupported.
//
// Example:
//
//	recent := conn.Table("orders").Where("total", ">", 100)
//	conn.Table("recent").WithMaterialized("recent", recent)
//	// WITH "recent" AS MATERIALIZED (SELECT * FROM "orders" WHERE "total" > $1) SELECT * FROM
//	//     "recent"
//	// args: [100]
//	// MySQL, SQLite and MariaDB return ErrUnsupported
func (b *Builder) WithMaterialized(name string, q *Builder, cols ...string) *Builder {
	return b.addCTE(cteClause{name: name, q: q, cols: cols, mat: 1})
}

// WithNotMaterialized adds "name as not materialized (q)". PostgreSQL only;
// other dialects return ErrUnsupported.
//
// Example:
//
//	recent := conn.Table("orders").Where("total", ">", 100)
//	conn.Table("recent").WithNotMaterialized("recent", recent)
//	// WITH "recent" AS NOT MATERIALIZED (SELECT * FROM "orders" WHERE "total" > $1) SELECT * FROM
//	//     "recent"
//	// args: [100]
//	// MySQL, SQLite and MariaDB return ErrUnsupported
func (b *Builder) WithNotMaterialized(name string, q *Builder, cols ...string) *Builder {
	return b.addCTE(cteClause{name: name, q: q, cols: cols, mat: -1})
}

func (b *Builder) addCTE(cte cteClause) *Builder {
	if cte.name == "" || strings.Contains(cte.name, ".") || aliasRe.MatchString(cte.name) {
		return b.fail(fmt.Errorf("%w: invalid CTE name %q", ErrInvalidArgs, cte.name))
	}
	if cte.q == nil && cte.raw == nil {
		return b.fail(fmt.Errorf("%w: nil CTE query %q", ErrInvalidArgs, cte.name))
	}
	if slices.Contains(cte.cols, "") {
		return b.fail(fmt.Errorf("%w: empty CTE column in %q", ErrInvalidArgs, cte.name))
	}
	for _, x := range b.ctes {
		if x.name == cte.name {
			return b.fail(fmt.Errorf("%w: duplicate CTE name %q", ErrInvalidArgs, cte.name))
		}
	}
	if cte.q != nil {
		cte.q = cte.q.Clone()
	}
	cte.cols = append([]string(nil), cte.cols...)
	b.ctes = append(b.ctes, cte)
	return b
}

func cloneCTEs(cs []cteClause) []cteClause {
	if cs == nil {
		return nil
	}
	out := make([]cteClause, len(cs))
	for i, cte := range cs {
		out[i] = cte
		out[i].cols = append([]string(nil), cte.cols...)
		if cte.q != nil {
			out[i].q = cte.q.Clone()
		}
	}
	return out
}

// with writes the "with [recursive] ..." prefix of a select, if any.
func (c *compiler) with(b *Builder) error {
	if len(b.ctes) == 0 {
		return nil
	}
	c.write("WITH ")
	for _, cte := range b.ctes {
		if cte.recursive {
			c.write("RECURSIVE ")
			break
		}
	}
	for i, cte := range b.ctes {
		if i > 0 {
			c.write(", ")
		}
		c.writeTable(cte.name)
		if len(cte.cols) > 0 {
			quoted := make([]string, len(cte.cols))
			for j, col := range cte.cols {
				quoted[j] = c.g.Quote(col)
			}
			c.write(" (" + strings.Join(quoted, ", ") + ")")
		}
		c.write(" AS ")
		if cte.mat != 0 {
			if c.d != Postgres {
				return fmt.Errorf("%w: materialized CTE on %s", ErrUnsupported, c.d)
			}
			if cte.mat < 0 {
				c.write("NOT ")
			}
			c.write("MATERIALIZED ")
		}
		c.write("(")
		if err := c.cteBody(cte); err != nil {
			return err
		}
		c.write(")")
	}
	c.write(" ")
	return nil
}

func (c *compiler) cteBody(cte cteClause) error {
	if cte.raw != nil {
		c.raw(*cte.raw)
		return nil
	}
	q := cte.q
	if q.err != nil {
		return q.err
	}
	if !cte.recursive || len(q.unions) == 0 {
		return c.selectQuery(q)
	}
	// A recursive reference must sit at the top level of its member (SQLite
	// rejects it inside a subquery), so the members are not wrapped.
	if err := c.with(q); err != nil {
		return err
	}
	if err := c.selectCore(q); err != nil {
		return err
	}
	for _, u := range q.unions {
		if u.q.err != nil {
			return u.q.err
		}
		if u.all {
			c.write(" UNION ALL ")
		} else {
			c.write(" UNION ")
		}
		if err := c.selectQuery(u.q); err != nil {
			return err
		}
	}
	return c.orderLimitOffset(q.unionOrders, q.unionLimit, q.unionOffset)
}
