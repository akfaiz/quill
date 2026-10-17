package db

import (
	"fmt"
	"slices"
	"strings"
)

// cteClause is one "name (cols) as (body)" entry of a WITH clause.
type cteClause struct {
	name string
	cols []string
	q    *query // the body, or the anchor of a recursive CTE
	raw  *Expression
	step *query // recursive member; non-nil makes the CTE recursive
	// unionDistinct joins anchor and step with UNION instead of UNION ALL.
	unionDistinct   bool
	materialized    bool
	notMaterialized bool
}

// CTEOption tunes a common table expression added with With, WithRaw or WithRecursive.
type CTEOption func(*cteClause)

// CTEColumns names the columns of the CTE: "with name (a, b) as (...)".
//
// Example:
//
//	conn.Select().From("a").With("a", conn.Select("id", "name").From("users"), db.CTEColumns("uid", "uname"))
//	// WITH "a" ("uid", "uname") AS (SELECT "id", "name" FROM "users") SELECT * FROM "a"
func CTEColumns(cols ...string) CTEOption {
	return func(c *cteClause) { c.cols = append(c.cols, cols...) }
}

// CTEMaterialized adds "as materialized": the CTE is computed once and reused.
// PostgreSQL 12+ and SQLite 3.35+; MySQL and MariaDB return ErrUnsupported.
// It cannot be combined with CTENotMaterialized.
//
// Example:
//
//	recent := conn.Select().From("orders").Where("total", ">", 100)
//	conn.Select().From("recent").With("recent", recent, db.CTEMaterialized())
//	// WITH "recent" AS MATERIALIZED (SELECT * FROM "orders" WHERE "total" > $1) SELECT * FROM
//	//     "recent"
//	// args: [100]
func CTEMaterialized() CTEOption {
	return func(c *cteClause) { c.materialized = true }
}

// CTENotMaterialized adds "as not materialized": the CTE may be inlined into the
// outer query. PostgreSQL 12+ and SQLite 3.35+; MySQL and MariaDB return
// ErrUnsupported. It cannot be combined with CTEMaterialized.
//
// Example:
//
//	recent := conn.Select().From("orders").Where("total", ">", 100)
//	conn.Select().From("recent").With("recent", recent, db.CTENotMaterialized())
//	// WITH "recent" AS NOT MATERIALIZED (SELECT * FROM "orders" WHERE "total" > $1) SELECT *
//	//     FROM "recent"
//	// args: [100]
func CTENotMaterialized() CTEOption {
	return func(c *cteClause) { c.notMaterialized = true }
}

// CTEUnionDistinct joins the anchor and the recursive member of WithRecursive with
// UNION (dropping duplicate rows) instead of UNION ALL.
//
// Example:
//
//	conn.Select().From("tree").WithRecursive("tree", anchor, step, db.CTEUnionDistinct())
//	// WITH RECURSIVE "tree" AS (<anchor> UNION <step>) SELECT * FROM "tree"
func CTEUnionDistinct() CTEOption {
	return func(c *cteClause) { c.unionDistinct = true }
}

// With adds a common table expression: "with name as (q)". The name is a table
// reference, so the table prefix applies to it as it does to From. The subquery
// is snapshotted: later changes to q do not affect this query. Options name the
// columns (CTEColumns) or hint materialization (CTEMaterialized, CTENotMaterialized).
//
// On a select, a CTE can feed the query, a subquery, a union member, Count,
// Exists and Paginate. On an update or delete it is written before the verb
// (PostgreSQL, MySQL 8, SQLite; MariaDB returns ErrUnsupported). On an insert it is
// moved into the SELECT given to Using, so it needs Using.
//
// Example:
//
//	active := conn.Select().From("users").Where("active", true)
//	conn.Select().From("active_users").With("active_users", active)
//	// WITH "active_users" AS (SELECT * FROM "users" WHERE "active" = $1) SELECT * FROM
//	//     "active_users"
//	// args: [true]
func (b *query) With(name string, q *query, opts ...CTEOption) *query {
	return b.addCTE(cteClause{name: name, q: q}, opts)
}

// WithRecursive adds a recursive common table expression and switches the clause
// to "with recursive". anchor is the starting query and step the recursive member
// that refers to name; they are joined with UNION ALL (CTEUnionDistinct for UNION)
// and written without parentheses, because SQLite rejects a recursive reference
// inside a subquery. Neither query is modified, and anchor must not have unions.
//
// Example:
//
//	anchor := conn.Select().From("categories").Where("parent_id", nil)
//	step := conn.Select().From("categories as c").Join("tree as t", "c.parent_id", "t.id")
//	conn.Select().From("tree").WithRecursive("tree", anchor, step)
//	// WITH RECURSIVE "tree" AS (SELECT * FROM "categories" WHERE "parent_id" IS NULL UNION ALL
//	//     SELECT * FROM "categories" AS "c" INNER JOIN "tree" AS "t" ON "c"."parent_id" =
//	//     "t"."id") SELECT * FROM "tree"
func (b *query) WithRecursive(name string, anchor, step *query, opts ...CTEOption) *query {
	if step == nil {
		return b.fail(fmt.Errorf("%w: nil recursive member for CTE %q", ErrInvalidArgs, name))
	}
	if anchor != nil && len(anchor.unions) > 0 {
		return b.fail(fmt.Errorf("%w: anchor of recursive CTE %q has unions", ErrInvalidArgs, name))
	}
	return b.addCTE(cteClause{name: name, q: anchor, step: step}, opts)
}

// WithRaw adds a common table expression whose body is a raw SQL fragment
// (caller-trusted SQL), e.g. db.Raw("values (?), (?)", 1, 2). It takes the same
// options as With.
//
// Example:
//
//	conn.Select().From("ids").WithRaw("ids", db.Raw("values (?), (?)", 1, 2), db.CTEColumns("id"))
//	// WITH "ids" ("id") AS (values ($1), ($2)) SELECT * FROM "ids"
//	// args: [1 2]
func (b *query) WithRaw(name string, e Expression, opts ...CTEOption) *query {
	return b.addCTE(cteClause{name: name, raw: &e}, opts)
}

func (b *query) addCTE(cte cteClause, opts []CTEOption) *query {
	for _, opt := range opts {
		if opt != nil {
			opt(&cte)
		}
	}
	if cte.name == "" || strings.Contains(cte.name, ".") || aliasRe.MatchString(cte.name) {
		return b.fail(fmt.Errorf("%w: invalid CTE name %q", ErrInvalidArgs, cte.name))
	}
	if cte.q == nil && cte.raw == nil {
		return b.fail(fmt.Errorf("%w: nil CTE query %q", ErrInvalidArgs, cte.name))
	}
	if cte.materialized && cte.notMaterialized {
		return b.fail(fmt.Errorf("%w: CTE %q is both materialized and not materialized", ErrInvalidArgs, cte.name))
	}
	if cte.unionDistinct && cte.step == nil {
		return b.fail(fmt.Errorf("%w: CTEUnionDistinct needs WithRecursive (CTE %q)", ErrInvalidArgs, cte.name))
	}
	if slices.Contains(cte.cols, "") {
		return b.fail(fmt.Errorf("%w: empty CTE column in %q", ErrInvalidArgs, cte.name))
	}
	if slices.ContainsFunc(b.ctes, func(x cteClause) bool { return x.name == cte.name }) {
		return b.fail(fmt.Errorf("%w: duplicate CTE name %q", ErrInvalidArgs, cte.name))
	}
	b.ctes = append(b.ctes, cte.clone())
	return b
}

func cloneCTEs(cs []cteClause) []cteClause {
	if cs == nil {
		return nil
	}
	out := make([]cteClause, len(cs))
	for i, cte := range cs {
		out[i] = cte.clone()
	}
	return out
}

// duplicateCTE returns the first repeated CTE name in cs, or "".
func duplicateCTE(cs []cteClause) string {
	for i, cte := range cs {
		if slices.ContainsFunc(cs[:i], func(x cteClause) bool { return x.name == cte.name }) {
			return cte.name
		}
	}
	return ""
}

// clone deep-copies the clause so later changes to the source queries cannot reach it.
func (cte cteClause) clone() cteClause {
	cte.cols = append([]string(nil), cte.cols...)
	if cte.q != nil {
		cte.q = cte.q.Clone()
	}
	if cte.step != nil {
		cte.step = cte.step.Clone()
	}
	return cte
}

// with writes the "with [recursive] ..." prefix of a select, if any.
func (c *compiler) with(b *query) error {
	if len(b.ctes) == 0 {
		return nil
	}
	c.write("WITH ")
	if slices.ContainsFunc(b.ctes, func(cte cteClause) bool { return cte.step != nil }) {
		c.write("RECURSIVE ")
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
		if cte.materialized || cte.notMaterialized {
			if c.d != Postgres && c.d != SQLite {
				return fmt.Errorf("%w: materialized CTE on %s", ErrUnsupported, c.d)
			}
			if cte.notMaterialized {
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
	if cte.step == nil {
		return c.selectQuery(q)
	}
	if cte.step.err != nil {
		return cte.step.err
	}
	// A recursive reference must sit at the top level of its member (SQLite
	// rejects it inside a subquery), so the members are not wrapped.
	if err := c.with(q); err != nil {
		return err
	}
	if err := c.selectCore(q); err != nil {
		return err
	}
	if cte.unionDistinct {
		c.write(" UNION ")
	} else {
		c.write(" UNION ALL ")
	}
	return c.selectQuery(cte.step)
}
