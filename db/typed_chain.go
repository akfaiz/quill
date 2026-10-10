package db

// A curated typed chain: each method forwards to the Builder method of the same
// name and returns the query so T is kept. Anything not listed here is
// reachable through Apply, e.g.
//
//	db.From[User](conn).Apply(func(b *db.Builder) { b.WhereDate("created_at", day) })

// Apply runs fn on the underlying builder, for any Builder method this type does not mirror.
func (q *Query[T]) Apply(fn func(*Builder)) *Query[T] {
	fn(q.b)
	return q
}

// When runs fn when cond is true.
func (q *Query[T]) When(cond bool, fn func(*Query[T])) *Query[T] {
	if cond {
		fn(q)
	}
	return q
}

// Unless runs fn when cond is false.
func (q *Query[T]) Unless(cond bool, fn func(*Query[T])) *Query[T] { return q.When(!cond, fn) }

// Clone returns an independent copy of the query.
func (q *Query[T]) Clone() *Query[T] { return &Query[T]{b: q.b.Clone()} }

// Select replaces the selected columns.
func (q *Query[T]) Select(columns ...string) *Query[T] {
	q.b.Select(columns...)
	return q
}

// AddSelect appends columns to the select list.
func (q *Query[T]) AddSelect(columns ...string) *Query[T] {
	q.b.AddSelect(columns...)
	return q
}

// SelectRaw appends a raw select expression (caller-trusted SQL).
func (q *Query[T]) SelectRaw(sql string, args ...any) *Query[T] {
	q.b.SelectRaw(sql, args...)
	return q
}

// Distinct makes the query SELECT DISTINCT.
func (q *Query[T]) Distinct() *Query[T] {
	q.b.Distinct()
	return q
}

// Where adds an AND predicate: Where(col, value) or Where(col, op, value).
func (q *Query[T]) Where(column string, args ...any) *Query[T] {
	q.b.Where(column, args...)
	return q
}

// OrWhere is Where joined with OR.
func (q *Query[T]) OrWhere(column string, args ...any) *Query[T] {
	q.b.OrWhere(column, args...)
	return q
}

// WhereNot adds "and not <predicate>".
func (q *Query[T]) WhereNot(column string, args ...any) *Query[T] {
	q.b.WhereNot(column, args...)
	return q
}

// WhereRaw adds a raw predicate (caller-trusted SQL).
func (q *Query[T]) WhereRaw(sql string, args ...any) *Query[T] {
	q.b.WhereRaw(sql, args...)
	return q
}

// OrWhereRaw is WhereRaw joined with OR.
func (q *Query[T]) OrWhereRaw(sql string, args ...any) *Query[T] {
	q.b.OrWhereRaw(sql, args...)
	return q
}

// WhereColumn compares two columns.
func (q *Query[T]) WhereColumn(first string, args ...any) *Query[T] {
	q.b.WhereColumn(first, args...)
	return q
}

// WhereIn adds "col in (...)"; values may be a slice or a *Builder subquery.
func (q *Query[T]) WhereIn(column string, values any) *Query[T] {
	q.b.WhereIn(column, values)
	return q
}

// OrWhereIn is WhereIn joined with OR.
func (q *Query[T]) OrWhereIn(column string, values any) *Query[T] {
	q.b.OrWhereIn(column, values)
	return q
}

// WhereNotIn adds "col not in (...)".
func (q *Query[T]) WhereNotIn(column string, values any) *Query[T] {
	q.b.WhereNotIn(column, values)
	return q
}

// WhereNull adds "col is null".
func (q *Query[T]) WhereNull(column string) *Query[T] {
	q.b.WhereNull(column)
	return q
}

// OrWhereNull is WhereNull joined with OR.
func (q *Query[T]) OrWhereNull(column string) *Query[T] {
	q.b.OrWhereNull(column)
	return q
}

// WhereNotNull adds "col is not null".
func (q *Query[T]) WhereNotNull(column string) *Query[T] {
	q.b.WhereNotNull(column)
	return q
}

// WhereBetween adds "col between lo and hi".
func (q *Query[T]) WhereBetween(column string, lo, hi any) *Query[T] {
	q.b.WhereBetween(column, lo, hi)
	return q
}

// WhereNotBetween adds "col not between lo and hi".
func (q *Query[T]) WhereNotBetween(column string, lo, hi any) *Query[T] {
	q.b.WhereNotBetween(column, lo, hi)
	return q
}

// WhereLike adds a LIKE predicate, case-insensitive unless caseSensitive[0].
func (q *Query[T]) WhereLike(column string, value any, caseSensitive ...bool) *Query[T] {
	q.b.WhereLike(column, value, caseSensitive...)
	return q
}

// WhereGroup wraps the predicates fn adds in parentheses.
func (q *Query[T]) WhereGroup(fn func(*Builder)) *Query[T] {
	q.b.WhereGroup(fn)
	return q
}

// OrWhereGroup is WhereGroup joined with OR.
func (q *Query[T]) OrWhereGroup(fn func(*Builder)) *Query[T] {
	q.b.OrWhereGroup(fn)
	return q
}

// WhereExists adds "exists (subquery)".
func (q *Query[T]) WhereExists(sub *Builder) *Query[T] {
	q.b.WhereExists(sub)
	return q
}

// WhereNotExists adds "not exists (subquery)".
func (q *Query[T]) WhereNotExists(sub *Builder) *Query[T] {
	q.b.WhereNotExists(sub)
	return q
}

// Join adds an inner join on a column comparison.
func (q *Query[T]) Join(table, first string, args ...any) *Query[T] {
	q.b.Join(table, first, args...)
	return q
}

// LeftJoin adds a left join.
func (q *Query[T]) LeftJoin(table, first string, args ...any) *Query[T] {
	q.b.LeftJoin(table, first, args...)
	return q
}

// RightJoin adds a right join.
func (q *Query[T]) RightJoin(table, first string, args ...any) *Query[T] {
	q.b.RightJoin(table, first, args...)
	return q
}

// JoinFunc adds an inner join with a complex ON clause.
func (q *Query[T]) JoinFunc(table string, fn func(*JoinClause)) *Query[T] {
	q.b.JoinFunc(table, fn)
	return q
}

// LeftJoinFunc adds a left join with a complex ON clause.
func (q *Query[T]) LeftJoinFunc(table string, fn func(*JoinClause)) *Query[T] {
	q.b.LeftJoinFunc(table, fn)
	return q
}

// GroupBy adds GROUP BY columns.
func (q *Query[T]) GroupBy(columns ...string) *Query[T] {
	q.b.GroupBy(columns...)
	return q
}

// Having adds an AND HAVING predicate.
func (q *Query[T]) Having(column string, args ...any) *Query[T] {
	q.b.Having(column, args...)
	return q
}

// HavingRaw adds a raw HAVING predicate (caller-trusted SQL).
func (q *Query[T]) HavingRaw(sql string, args ...any) *Query[T] {
	q.b.HavingRaw(sql, args...)
	return q
}

// OrderBy adds ORDER BY; direction is "asc" (default) or "desc".
func (q *Query[T]) OrderBy(column string, direction ...string) *Query[T] {
	q.b.OrderBy(column, direction...)
	return q
}

// OrderByDesc is OrderBy(column, "desc").
func (q *Query[T]) OrderByDesc(column string) *Query[T] {
	q.b.OrderByDesc(column)
	return q
}

// OrderByRaw adds a raw ORDER BY expression (caller-trusted SQL).
func (q *Query[T]) OrderByRaw(sql string, args ...any) *Query[T] {
	q.b.OrderByRaw(sql, args...)
	return q
}

// Latest orders by column descending (default "created_at").
func (q *Query[T]) Latest(column ...string) *Query[T] {
	q.b.Latest(column...)
	return q
}

// Oldest orders by column ascending (default "created_at").
func (q *Query[T]) Oldest(column ...string) *Query[T] {
	q.b.Oldest(column...)
	return q
}

// Limit sets LIMIT.
func (q *Query[T]) Limit(n int) *Query[T] {
	q.b.Limit(n)
	return q
}

// Offset sets OFFSET.
func (q *Query[T]) Offset(n int) *Query[T] {
	q.b.Offset(n)
	return q
}

// ForPage sets offset and limit for a 1-based page.
func (q *Query[T]) ForPage(page, perPage int) *Query[T] {
	q.b.ForPage(page, perPage)
	return q
}

// LockForUpdate adds "for update" (PostgreSQL, MySQL).
func (q *Query[T]) LockForUpdate() *Query[T] {
	q.b.LockForUpdate()
	return q
}

// SharedLock adds a shared row lock (PostgreSQL, MySQL).
func (q *Query[T]) SharedLock() *Query[T] {
	q.b.SharedLock()
	return q
}

// AllRows confirms that an update or delete without a where clause is intended.
func (q *Query[T]) AllRows() *Query[T] {
	q.b.AllRows()
	return q
}

// Returning adds a RETURNING clause (PostgreSQL, SQLite).
func (q *Query[T]) Returning(columns ...string) *Query[T] {
	q.b.Returning(columns...)
	return q
}

// With adds a common table expression "with name (cols) as (q)".
func (q *Query[T]) With(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.With(name, sub, cols...)
	return q
}

// WithRecursive adds a recursive common table expression.
func (q *Query[T]) WithRecursive(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.WithRecursive(name, sub, cols...)
	return q
}

// WithRaw adds a common table expression with a raw SQL body (caller-trusted SQL).
func (q *Query[T]) WithRaw(name string, e Expression, cols ...string) *Query[T] {
	q.b.WithRaw(name, e, cols...)
	return q
}

// WithMaterialized adds "name as materialized (q)" (PostgreSQL only).
func (q *Query[T]) WithMaterialized(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.WithMaterialized(name, sub, cols...)
	return q
}

// WithNotMaterialized adds "name as not materialized (q)" (PostgreSQL only).
func (q *Query[T]) WithNotMaterialized(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.WithNotMaterialized(name, sub, cols...)
	return q
}
