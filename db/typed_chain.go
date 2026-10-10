package db

// A curated typed chain: each method forwards to the Builder method of the same
// name and returns the query so T is kept. Anything not listed here is
// reachable through Apply, e.g.
//
//	db.From[User](conn).Apply(func(b *db.Builder) { b.WhereDate("created_at", day) })

// Apply runs fn on the underlying builder, for any Builder method this type does not mirror.
//
// Example:
//
//	db.From[User](conn).Apply(func(b *db.Builder) { b.WhereDate("created_at", day) })
//	// SELECT * FROM "users" WHERE "created_at"::DATE = $1   args: [2026-01-02]
func (q *Query[T]) Apply(fn func(*Builder)) *Query[T] {
	fn(q.b)
	return q
}

// When runs fn when cond is true.
//
// Example:
//
//	db.From[User](conn).When(onlyAdults, func(q *db.Query[User]) { q.Where("age", ">=", 18) })
//	// onlyAdults == true: SELECT * FROM "users" WHERE "age" >= $1   args: [18]
func (q *Query[T]) When(cond bool, fn func(*Query[T])) *Query[T] {
	if cond {
		fn(q)
	}
	return q
}

// Unless runs fn when cond is false.
//
// Example:
//
//	db.From[User](conn).Unless(includeMinors, func(q *db.Query[User]) { q.Where("age", ">=", 18) })
//	// includeMinors == false: SELECT * FROM "users" WHERE "age" >= $1   args: [18]
func (q *Query[T]) Unless(cond bool, fn func(*Query[T])) *Query[T] { return q.When(!cond, fn) }

// Clone returns an independent copy of the query.
//
// Example:
//
//	base := db.From[User](conn).Where("age", ">=", 18)
//	refined := base.Clone().OrderBy("name")
//	// refined: SELECT * FROM "users" WHERE "age" >= $1 ORDER BY "name" ASC   args: [18]
//	// base is unchanged: SELECT * FROM "users" WHERE "age" >= $1   args: [18]
func (q *Query[T]) Clone() *Query[T] { return &Query[T]{b: q.b.Clone()} }

// Select replaces the selected columns.
//
// Example:
//
//	db.From[User](conn).Select("id", "name")
//	// SELECT "id", "name" FROM "users"   args: []
func (q *Query[T]) Select(columns ...string) *Query[T] {
	q.b.Select(columns...)
	return q
}

// AddSelect appends columns to the select list.
//
// Example:
//
//	db.From[User](conn).Select("id", "name").AddSelect("age")
//	// SELECT "id", "name", "age" FROM "users"   args: []
func (q *Query[T]) AddSelect(columns ...string) *Query[T] {
	q.b.AddSelect(columns...)
	return q
}

// SelectRaw appends a raw select expression (caller-trusted SQL).
//
// Example:
//
//	db.From[User](conn).SelectRaw("upper(name) AS shout, age + ? AS next", 1)
//	// SELECT upper(name) AS shout, age + $1 AS next FROM "users"   args: [1]
func (q *Query[T]) SelectRaw(sql string, args ...any) *Query[T] {
	q.b.SelectRaw(sql, args...)
	return q
}

// Distinct makes the query SELECT DISTINCT.
//
// Example:
//
//	db.From[User](conn).Select("name").Distinct()
//	// SELECT DISTINCT "name" FROM "users"   args: []
func (q *Query[T]) Distinct() *Query[T] {
	q.b.Distinct()
	return q
}

// Where adds an AND predicate: Where(col, value) or Where(col, op, value).
//
// Example:
//
//	db.From[User](conn).Where("age", ">", 18)
//	// SELECT * FROM "users" WHERE "age" > $1   args: [18]
func (q *Query[T]) Where(column string, args ...any) *Query[T] {
	q.b.Where(column, args...)
	return q
}

// OrWhere is Where joined with OR.
//
// Example:
//
//	db.From[User](conn).Where("age", ">", 18).OrWhere("name", "ann")
//	// SELECT * FROM "users" WHERE "age" > $1 OR "name" = $2   args: [18 ann]
func (q *Query[T]) OrWhere(column string, args ...any) *Query[T] {
	q.b.OrWhere(column, args...)
	return q
}

// WhereNot adds "AND NOT <predicate>".
//
// Example:
//
//	db.From[User](conn).WhereNot("id", 3)
//	// SELECT * FROM "users" WHERE NOT "id" = $1   args: [3]
func (q *Query[T]) WhereNot(column string, args ...any) *Query[T] {
	q.b.WhereNot(column, args...)
	return q
}

// WhereRaw adds a raw predicate (caller-trusted SQL).
//
// Example:
//
//	db.From[User](conn).WhereRaw("age % ? = 0", 2)
//	// SELECT * FROM "users" WHERE age % $1 = 0   args: [2]
func (q *Query[T]) WhereRaw(sql string, args ...any) *Query[T] {
	q.b.WhereRaw(sql, args...)
	return q
}

// OrWhereRaw is WhereRaw joined with OR.
//
// Example:
//
//	db.From[User](conn).WhereRaw("age % ? = 0", 2).OrWhereRaw("length(name) > ?", 10)
//	// SELECT * FROM "users" WHERE age % $1 = 0 OR length(name) > $2   args: [2 10]
func (q *Query[T]) OrWhereRaw(sql string, args ...any) *Query[T] {
	q.b.OrWhereRaw(sql, args...)
	return q
}

// WhereColumn compares two columns.
//
// Example:
//
//	db.From[User](conn).WhereColumn("created_at", "<", "updated_at")
//	// SELECT * FROM "users" WHERE "created_at" < "updated_at"   args: []
func (q *Query[T]) WhereColumn(first string, args ...any) *Query[T] {
	q.b.WhereColumn(first, args...)
	return q
}

// WhereIn adds "col IN (...)"; values may be a slice or a *Builder subquery.
//
// Example:
//
//	db.From[User](conn).WhereIn("id", []int{1, 2, 3})
//	// SELECT * FROM "users" WHERE "id" IN ($1, $2, $3)   args: [1 2 3]
func (q *Query[T]) WhereIn(column string, values any) *Query[T] {
	q.b.WhereIn(column, values)
	return q
}

// OrWhereIn is WhereIn joined with OR.
//
// Example:
//
//	db.From[User](conn).WhereIn("id", []int{1, 2}).OrWhereIn("name", []string{"ann"})
//	// SELECT * FROM "users" WHERE "id" IN ($1, $2) OR "name" IN ($3)   args: [1 2 ann]
func (q *Query[T]) OrWhereIn(column string, values any) *Query[T] {
	q.b.OrWhereIn(column, values)
	return q
}

// WhereNotIn adds "col NOT IN (...)".
//
// Example:
//
//	db.From[User](conn).WhereNotIn("age", []int{0})
//	// SELECT * FROM "users" WHERE "age" NOT IN ($1)   args: [0]
func (q *Query[T]) WhereNotIn(column string, values any) *Query[T] {
	q.b.WhereNotIn(column, values)
	return q
}

// WhereNull adds "col IS NULL".
//
// Example:
//
//	db.From[User](conn).WhereNull("deleted_at")
//	// SELECT * FROM "users" WHERE "deleted_at" IS NULL   args: []
func (q *Query[T]) WhereNull(column string) *Query[T] {
	q.b.WhereNull(column)
	return q
}

// OrWhereNull is WhereNull joined with OR.
//
// Example:
//
//	db.From[User](conn).WhereNull("deleted_at").OrWhereNull("archived_at")
//	// SELECT * FROM "users" WHERE "deleted_at" IS NULL OR "archived_at" IS NULL   args: []
func (q *Query[T]) OrWhereNull(column string) *Query[T] {
	q.b.OrWhereNull(column)
	return q
}

// WhereNotNull adds "col IS NOT NULL".
//
// Example:
//
//	db.From[User](conn).WhereNotNull("name")
//	// SELECT * FROM "users" WHERE "name" IS NOT NULL   args: []
func (q *Query[T]) WhereNotNull(column string) *Query[T] {
	q.b.WhereNotNull(column)
	return q
}

// WhereBetween adds "col BETWEEN lo AND hi".
//
// Example:
//
//	db.From[User](conn).WhereBetween("age", 18, 65)
//	// SELECT * FROM "users" WHERE "age" BETWEEN $1 AND $2   args: [18 65]
func (q *Query[T]) WhereBetween(column string, lo, hi any) *Query[T] {
	q.b.WhereBetween(column, lo, hi)
	return q
}

// WhereNotBetween adds "col NOT BETWEEN lo AND hi".
//
// Example:
//
//	db.From[User](conn).WhereNotBetween("id", 10, 20)
//	// SELECT * FROM "users" WHERE "id" NOT BETWEEN $1 AND $2   args: [10 20]
func (q *Query[T]) WhereNotBetween(column string, lo, hi any) *Query[T] {
	q.b.WhereNotBetween(column, lo, hi)
	return q
}

// WhereLike adds a LIKE predicate, case-insensitive unless caseSensitive[0].
//
// Example:
//
//	db.From[User](conn).WhereLike("name", "an%")
//	// SELECT * FROM "users" WHERE "name"::TEXT ILIKE $1   args: [an%]
//	db.From[User](conn).WhereLike("name", "An%", true) // case-sensitive
//	// SELECT * FROM "users" WHERE "name"::TEXT LIKE $1   args: [An%]
func (q *Query[T]) WhereLike(column string, value any, caseSensitive ...bool) *Query[T] {
	q.b.WhereLike(column, value, caseSensitive...)
	return q
}

// WhereGroup wraps the predicates fn adds in parentheses.
//
// Example:
//
//	db.From[User](conn).Where("age", ">", 18).WhereGroup(func(b *db.Builder) {
//		b.Where("name", "ann").OrWhere("name", "bob")
//	})
//	// SELECT * FROM "users" WHERE "age" > $1 AND ("name" = $2 OR "name" = $3)   args: [18 ann bob]
func (q *Query[T]) WhereGroup(fn func(*Builder)) *Query[T] {
	q.b.WhereGroup(fn)
	return q
}

// OrWhereGroup is WhereGroup joined with OR.
//
// Example:
//
//	db.From[User](conn).Where("age", ">", 18).OrWhereGroup(func(b *db.Builder) { b.Where("id", 99) })
//	// SELECT * FROM "users" WHERE "age" > $1 OR ("id" = $2)   args: [18 99]
func (q *Query[T]) OrWhereGroup(fn func(*Builder)) *Query[T] {
	q.b.OrWhereGroup(fn)
	return q
}

// WhereExists adds "EXISTS (subquery)".
//
// Example:
//
//	posts := conn.Table("posts").WhereColumn("posts.user_id", "=", "users.id")
//	db.From[User](conn).WhereExists(posts)
//	// SELECT * FROM "users" WHERE EXISTS
//	//   (SELECT * FROM "posts" WHERE "posts"."user_id" = "users"."id")   args: []
func (q *Query[T]) WhereExists(sub *Builder) *Query[T] {
	q.b.WhereExists(sub)
	return q
}

// WhereNotExists adds "NOT EXISTS (subquery)".
//
// Example:
//
//	posts := conn.Table("posts").WhereColumn("posts.user_id", "=", "users.id")
//	db.From[User](conn).WhereNotExists(posts)
//	// SELECT * FROM "users" WHERE NOT EXISTS
//	//   (SELECT * FROM "posts" WHERE "posts"."user_id" = "users"."id")   args: []
func (q *Query[T]) WhereNotExists(sub *Builder) *Query[T] {
	q.b.WhereNotExists(sub)
	return q
}

// Join adds an inner join on a column comparison.
//
// Example:
//
//	db.From[User](conn).Select("users.name", "p.title").Join("posts as p", "p.user_id", "users.id")
//	// SELECT "users"."name", "p"."title" FROM "users"
//	//   INNER JOIN "posts" AS "p" ON "p"."user_id" = "users"."id"   args: []
func (q *Query[T]) Join(table, first string, args ...any) *Query[T] {
	q.b.Join(table, first, args...)
	return q
}

// LeftJoin adds a left join.
//
// Example:
//
//	db.From[User](conn).LeftJoin("posts as q", "q.user_id", "users.id")
//	// SELECT * FROM "users" LEFT JOIN "posts" AS "q" ON "q"."user_id" = "users"."id"   args: []
func (q *Query[T]) LeftJoin(table, first string, args ...any) *Query[T] {
	q.b.LeftJoin(table, first, args...)
	return q
}

// RightJoin adds a right join.
//
// Example:
//
//	db.From[User](conn).RightJoin("posts as r", "r.user_id", "users.id")
//	// SELECT * FROM "users" RIGHT JOIN "posts" AS "r" ON "r"."user_id" = "users"."id"   args: []
func (q *Query[T]) RightJoin(table, first string, args ...any) *Query[T] {
	q.b.RightJoin(table, first, args...)
	return q
}

// JoinFunc adds an inner join with a complex ON clause.
//
// Example:
//
//	db.From[User](conn).JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "users.id").Where("p.title", "hello")
//	})
//	// SELECT * FROM "users" INNER JOIN "posts" AS "p"
//	//   ON "p"."user_id" = "users"."id" AND "p"."title" = $1   args: [hello]
func (q *Query[T]) JoinFunc(table string, fn func(*JoinClause)) *Query[T] {
	q.b.JoinFunc(table, fn)
	return q
}

// LeftJoinFunc adds a left join with a complex ON clause.
//
// Example:
//
//	db.From[User](conn).LeftJoinFunc("posts as q", func(j *db.JoinClause) {
//		j.On("q.user_id", "users.id").WhereNull("q.id")
//	})
//	// SELECT * FROM "users" LEFT JOIN "posts" AS "q"
//	//   ON "q"."user_id" = "users"."id" AND "q"."id" IS NULL   args: []
func (q *Query[T]) LeftJoinFunc(table string, fn func(*JoinClause)) *Query[T] {
	q.b.LeftJoinFunc(table, fn)
	return q
}

// GroupBy adds GROUP BY columns.
//
// Example:
//
//	db.From[User](conn).Select("age").SelectRaw("count(*) AS n").GroupBy("age")
//	// SELECT "age", count(*) AS n FROM "users" GROUP BY "age"   args: []
func (q *Query[T]) GroupBy(columns ...string) *Query[T] {
	q.b.GroupBy(columns...)
	return q
}

// Having adds an AND HAVING predicate.
//
// Example:
//
//	db.From[User](conn).Select("age").GroupBy("age").Having("age", ">", 18)
//	// SELECT "age" FROM "users" GROUP BY "age" HAVING "age" > $1   args: [18]
func (q *Query[T]) Having(column string, args ...any) *Query[T] {
	q.b.Having(column, args...)
	return q
}

// HavingRaw adds a raw HAVING predicate (caller-trusted SQL).
//
// Example:
//
//	db.From[User](conn).Select("age").GroupBy("age").HavingRaw("count(*) > ?", 1)
//	// SELECT "age" FROM "users" GROUP BY "age" HAVING count(*) > $1   args: [1]
func (q *Query[T]) HavingRaw(sql string, args ...any) *Query[T] {
	q.b.HavingRaw(sql, args...)
	return q
}

// OrderBy adds ORDER BY; direction is "asc" (default) or "desc".
//
// Example:
//
//	db.From[User](conn).OrderBy("name").OrderBy("age", "desc")
//	// SELECT * FROM "users" ORDER BY "name" ASC, "age" DESC   args: []
func (q *Query[T]) OrderBy(column string, direction ...string) *Query[T] {
	q.b.OrderBy(column, direction...)
	return q
}

// OrderByDesc is OrderBy(column, "desc").
//
// Example:
//
//	db.From[User](conn).OrderByDesc("id")
//	// SELECT * FROM "users" ORDER BY "id" DESC   args: []
func (q *Query[T]) OrderByDesc(column string) *Query[T] {
	q.b.OrderByDesc(column)
	return q
}

// OrderByRaw adds a raw ORDER BY expression (caller-trusted SQL).
//
// Example:
//
//	db.From[User](conn).OrderByRaw("random()")
//	// SELECT * FROM "users" ORDER BY random()   args: []
func (q *Query[T]) OrderByRaw(sql string, args ...any) *Query[T] {
	q.b.OrderByRaw(sql, args...)
	return q
}

// Latest orders by column descending (default "created_at").
//
// Example:
//
//	db.From[User](conn).Latest()
//	// SELECT * FROM "users" ORDER BY "created_at" DESC   args: []
func (q *Query[T]) Latest(column ...string) *Query[T] {
	q.b.Latest(column...)
	return q
}

// Oldest orders by column ascending (default "created_at").
//
// Example:
//
//	db.From[User](conn).Oldest("created_at")
//	// SELECT * FROM "users" ORDER BY "created_at" ASC   args: []
func (q *Query[T]) Oldest(column ...string) *Query[T] {
	q.b.Oldest(column...)
	return q
}

// Limit sets LIMIT.
//
// Example:
//
//	db.From[User](conn).OrderBy("id").Limit(10)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 10   args: []
func (q *Query[T]) Limit(n int) *Query[T] {
	q.b.Limit(n)
	return q
}

// Offset sets OFFSET.
//
// Example:
//
//	db.From[User](conn).OrderBy("id").Limit(10).Offset(20)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 10 OFFSET 20   args: []
func (q *Query[T]) Offset(n int) *Query[T] {
	q.b.Offset(n)
	return q
}

// ForPage sets offset and limit for a 1-based page.
//
// Example:
//
//	db.From[User](conn).OrderBy("id").ForPage(3, 15)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 15 OFFSET 30   args: []
func (q *Query[T]) ForPage(page, perPage int) *Query[T] {
	q.b.ForPage(page, perPage)
	return q
}

// LockForUpdate adds "FOR UPDATE" (PostgreSQL, MySQL).
//
// Example:
//
//	db.From[User](conn).Where("id", 1).LockForUpdate()
//	// SELECT * FROM "users" WHERE "id" = $1 FOR UPDATE   args: [1]
func (q *Query[T]) LockForUpdate() *Query[T] {
	q.b.LockForUpdate()
	return q
}

// SharedLock adds a shared row lock (PostgreSQL, MySQL).
//
// Example:
//
//	db.From[User](conn).Where("id", 1).SharedLock()
//	// SELECT * FROM "users" WHERE "id" = $1 FOR SHARE   args: [1]
func (q *Query[T]) SharedLock() *Query[T] {
	q.b.SharedLock()
	return q
}

// AllRows confirms that an update or delete without a where clause is intended.
//
// Example:
//
//	n, err := db.From[User](conn).AllRows().UpdateMap(ctx, map[string]any{"age": 0})
//	// UPDATE "users" SET "age" = $1   args: [0]
//	// Without AllRows (and without Where) err is "update/delete without a where clause"
func (q *Query[T]) AllRows() *Query[T] {
	q.b.AllRows()
	return q
}

// Returning adds a RETURNING clause (PostgreSQL, SQLite).
//
// Example:
//
//	q := db.From[User](conn).Where("id", 1).Returning("id")
//	rows, err := q.UpdateReturning(ctx, User{Name: "ann", Age: 31})
//	// UPDATE "users" SET "age" = $1, "name" = $2 WHERE "id" = $3 RETURNING "id"   args: [31 ann 1]
func (q *Query[T]) Returning(columns ...string) *Query[T] {
	q.b.Returning(columns...)
	return q
}

// With adds a common table expression "with name (cols) as (q)".
//
// Example:
//
//	adults := conn.Table("users").Where("age", ">=", 18)
//	db.As[User](conn.Table("adults")).With("adults", adults)
//	// WITH "adults" AS (SELECT * FROM "users" WHERE "age" >= $1) SELECT * FROM "adults"   args: [18]
func (q *Query[T]) With(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.With(name, sub, cols...)
	return q
}

// WithRecursive adds a recursive common table expression.
//
// Example:
//
//	seed := conn.Table("users").Select("id", "name").Where("id", 1)
//	db.As[User](conn.Table("tree")).WithRecursive("tree", seed, "id", "name")
//	// WITH RECURSIVE "tree" ("id", "name") AS
//	//   (SELECT "id", "name" FROM "users" WHERE "id" = $1) SELECT * FROM "tree"   args: [1]
func (q *Query[T]) WithRecursive(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.WithRecursive(name, sub, cols...)
	return q
}

// WithRaw adds a common table expression with a raw SQL body (caller-trusted SQL).
//
// Example:
//
//	db.As[User](conn.Table("nums")).
//		WithRaw("nums", db.Raw("SELECT generate_series(1, ?) AS id", 3), "id")
//	// WITH "nums" ("id") AS (SELECT generate_series(1, $1) AS id) SELECT * FROM "nums"   args: [3]
func (q *Query[T]) WithRaw(name string, e Expression, cols ...string) *Query[T] {
	q.b.WithRaw(name, e, cols...)
	return q
}

// WithMaterialized adds "name as materialized (q)" (PostgreSQL only).
//
// Example:
//
//	adults := conn.Table("users").Where("age", ">=", 18)
//	db.As[User](conn.Table("adults")).WithMaterialized("adults", adults)
//	// WITH "adults" AS MATERIALIZED (SELECT * FROM "users" WHERE "age" >= $1)
//	//   SELECT * FROM "adults"   args: [18]
func (q *Query[T]) WithMaterialized(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.WithMaterialized(name, sub, cols...)
	return q
}

// WithNotMaterialized adds "name as not materialized (q)" (PostgreSQL only).
//
// Example:
//
//	adults := conn.Table("users").Where("age", ">=", 18)
//	db.As[User](conn.Table("adults")).WithNotMaterialized("adults", adults)
//	// WITH "adults" AS NOT MATERIALIZED (SELECT * FROM "users" WHERE "age" >= $1)
//	//   SELECT * FROM "adults"   args: [18]
func (q *Query[T]) WithNotMaterialized(name string, sub *Builder, cols ...string) *Query[T] {
	q.b.WithNotMaterialized(name, sub, cols...)
	return q
}
