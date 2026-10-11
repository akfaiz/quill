package db

import (
	"fmt"
	"strings"
)

type joinClause struct {
	typ     string // inner, left, right, cross, straight_join
	table   string
	sub     *query // subquery source; alias is required
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
type JoinClause struct{ q *query }

// On adds "first = second" or "first op second" (column to column).
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").On("p.created_at", ">=", "u.created_at")
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."created_at" >= "u"."created_at"
func (j *JoinClause) On(first string, args ...any) *JoinClause {
	j.q.columnClause("and", first, args)
	return j
}

// OrOn is On joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OrOn("p.editor_id", "u.id")
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" OR
//	//     "p"."editor_id" = "u"."id"
func (j *JoinClause) OrOn(first string, args ...any) *JoinClause {
	j.q.columnClause("or", first, args)
	return j
}

// OnGroup wraps the conditions added inside fn in parentheses.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OnGroup(func(g *db.JoinClause) {
//			g.Where("p.status", "published").OrWhere("p.status", "draft")
//		})
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     ("p"."status" = $1 OR "p"."status" = $2)
//	// args: [published draft]
func (j *JoinClause) OnGroup(fn func(*JoinClause)) *JoinClause {
	j.q.WhereGroup(func(q *query) { fn(&JoinClause{q}) })
	return j
}

// OrOnGroup is OnGroup joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OrOnGroup(func(g *db.JoinClause) {
//			g.On("p.editor_id", "u.id").Where("p.score", ">", 10)
//		})
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" OR
//	//     ("p"."editor_id" = "u"."id" AND "p"."score" > $1)
//	// args: [10]
func (j *JoinClause) OrOnGroup(fn func(*JoinClause)) *JoinClause {
	j.q.OrWhereGroup(func(q *query) { fn(&JoinClause{q}) })
	return j
}

// Where adds a bound-value condition: Where(col, value) or Where(col, op, value).
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").Where("p.status", "published").Where("p.score", ">", 10)
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."status" = $1 AND "p"."score" > $2
//	// args: [published 10]
func (j *JoinClause) Where(column string, args ...any) *JoinClause {
	j.q.Where(column, args...)
	return j
}

// OrWhere is Where joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").Where("p.status", "published").OrWhere("p.pinned", true)
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."status" = $1 OR "p"."pinned" = $2
//	// args: [published true]
func (j *JoinClause) OrWhere(column string, args ...any) *JoinClause {
	j.q.OrWhere(column, args...)
	return j
}

// WhereIn adds "col IN (...)".
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").WhereIn("p.status", []string{"published", "draft"})
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."status" IN ($1, $2)
//	// args: [published draft]
func (j *JoinClause) WhereIn(column string, values any) *JoinClause {
	j.q.WhereIn(column, values)
	return j
}

// WhereNotIn adds "col NOT IN (...)".
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").WhereNotIn("p.status", []string{"spam"})
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."status" NOT IN ($1)
//	// args: [spam]
func (j *JoinClause) WhereNotIn(column string, values any) *JoinClause {
	j.q.WhereNotIn(column, values)
	return j
}

// WhereNull adds "col IS NULL".
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").WhereNull("p.deleted_at")
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."deleted_at" IS NULL
func (j *JoinClause) WhereNull(column string) *JoinClause {
	j.q.WhereNull(column)
	return j
}

// WhereNotNull adds "col IS NOT NULL".
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").WhereNotNull("p.published_at")
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."published_at" IS NOT NULL
func (j *JoinClause) WhereNotNull(column string) *JoinClause {
	j.q.WhereNotNull(column)
	return j
}

// WhereRaw adds a raw condition (caller-trusted SQL).
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").WhereRaw("p.score > ?", 10)
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     p.score > $1
//	// args: [10]
func (j *JoinClause) WhereRaw(sql string, args ...any) *JoinClause {
	j.q.WhereRaw(sql, args...)
	return j
}

// OrWhereRaw is WhereRaw joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OrWhereRaw("p.score > ?", 10)
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" OR
//	//     p.score > $1
//	// args: [10]
func (j *JoinClause) OrWhereRaw(sql string, args ...any) *JoinClause {
	j.q.OrWhereRaw(sql, args...)
	return j
}

// addJoin resolves the ON conditions from fn, or from (first, args) as a column
// comparison (where=false) or a value comparison (where=true).
func (b *query) addJoin(j joinClause, first string, args []any, where bool, fn func(*JoinClause)) *query {
	if fn != nil {
		return b.addJoinFunc(j, fn)
	}
	// A single ON condition needs no real builder: collect it in a scratch value
	// that stays on the stack instead of allocating a full query per join.
	var q query
	q.dialect, q.prefix, q.err = b.dialect, b.prefix, b.err
	if first != "" || len(args) > 0 {
		if where {
			q.addClause(&q.wheres, "and", first, args)
		} else {
			q.columnClause("and", first, args)
		}
	}
	return b.finishJoin(j, q.wheres, q.err)
}

// addJoinFunc resolves the ON conditions from a JoinClause callback, which gets
// a real builder because it may add any number of conditions and groups.
func (b *query) addJoinFunc(j joinClause, fn func(*JoinClause)) *query {
	q := b.fresh()
	fn(&JoinClause{q})
	return b.finishJoin(j, q.wheres, q.err)
}

func (b *query) finishJoin(j joinClause, on []clause, err error) *query {
	if err != nil {
		return b.fail(err)
	}
	if j.sub == nil && j.table == "" {
		return b.fail(fmt.Errorf("%w: join needs a table or a non-nil subquery", ErrInvalidArgs))
	}
	j.on = on
	b.joins = append(b.joins, j)
	return b
}

func tbl(typ, table string) joinClause { return joinClause{typ: typ, table: table} }

func subJoin(typ string, sub *query, as string) joinClause {
	if sub != nil {
		sub = sub.Clone() // snapshot, see snapshot() in where.go
	}
	return joinClause{typ: typ, sub: sub, alias: as}
}

// Join adds "INNER JOIN table ON first = second" (or "first op second").
//
// Example:
//
//	conn.Select().From("users as u").Join("posts as p", "p.user_id", "u.id")
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id"
func (b *query) Join(table, first string, args ...any) *query {
	return b.addJoin(tbl("inner", table), first, args, false, nil)
}

// LeftJoin adds a left join.
//
// Example:
//
//	conn.Select().From("users as u").LeftJoin("posts as p", "p.user_id", "u.id")
//	// SELECT * FROM "users" AS "u" LEFT JOIN "posts" AS "p" ON "p"."user_id" = "u"."id"
func (b *query) LeftJoin(table, first string, args ...any) *query {
	return b.addJoin(tbl("left", table), first, args, false, nil)
}

// RightJoin adds a right join.
//
// Example:
//
//	conn.Select().From("users as u").RightJoin("posts as p", "p.user_id", "u.id")
//	// SELECT * FROM "users" AS "u" RIGHT JOIN "posts" AS "p" ON "p"."user_id" = "u"."id"
func (b *query) RightJoin(table, first string, args ...any) *query {
	return b.addJoin(tbl("right", table), first, args, false, nil)
}

// CrossJoin adds "CROSS JOIN table".
//
// Example:
//
//	conn.Select().From("users").CrossJoin("roles")
//	// SELECT * FROM "users" CROSS JOIN "roles"
func (b *query) CrossJoin(table string) *query {
	return b.addJoin(tbl("cross", table), "", nil, false, nil)
}

// StraightJoin adds a MySQL "straight_join"; other dialects error at compile time.
//
// Example:
//
//	connMy.Select().From("users as u").StraightJoin("posts as p", "p.user_id", "u.id")
//	// SELECT * FROM `users` AS `u` STRAIGHT_JOIN `posts` AS `p` ON `p`.`user_id` = `u`.`id`
//	// connMy is db.Use(db.MySQL); other dialects return ErrUnsupported
func (b *query) StraightJoin(table, first string, args ...any) *query {
	return b.addJoin(tbl("straight_join", table), first, args, false, nil)
}

// JoinFunc adds an inner join with a complex ON clause built in fn.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").Where("p.published", true)
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."published" = $1
//	// args: [true]
func (b *query) JoinFunc(table string, fn func(*JoinClause)) *query {
	return b.addJoin(tbl("inner", table), "", nil, false, fn)
}

// LeftJoinFunc adds a left join with a complex ON clause built in fn.
//
// Example:
//
//	conn.Select().From("users as u").LeftJoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").WhereNull("p.deleted_at")
//	})
//	// SELECT * FROM "users" AS "u" LEFT JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."deleted_at" IS NULL
func (b *query) LeftJoinFunc(table string, fn func(*JoinClause)) *query {
	return b.addJoin(tbl("left", table), "", nil, false, fn)
}

// RightJoinFunc adds a right join with a complex ON clause built in fn.
//
// Example:
//
//	conn.Select().From("users as u").RightJoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").Where("p.score", ">", 10)
//	})
//	// SELECT * FROM "users" AS "u" RIGHT JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" AND
//	//     "p"."score" > $1
//	// args: [10]
func (b *query) RightJoinFunc(table string, fn func(*JoinClause)) *query {
	return b.addJoin(tbl("right", table), "", nil, false, fn)
}

// JoinWhere joins on a bound value: "INNER JOIN t ON col = ?".
//
// Example:
//
//	conn.Select().From("users as u").JoinWhere("posts as p", "p.status", "published")
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."status" = $1
//	// args: [published]
func (b *query) JoinWhere(table, column string, args ...any) *query {
	return b.addJoin(tbl("inner", table), column, args, true, nil)
}

// LeftJoinWhere is JoinWhere as a left join.
//
// Example:
//
//	conn.Select().From("users as u").LeftJoinWhere("posts as p", "p.score", ">", 10)
//	// SELECT * FROM "users" AS "u" LEFT JOIN "posts" AS "p" ON "p"."score" > $1
//	// args: [10]
func (b *query) LeftJoinWhere(table, column string, args ...any) *query {
	return b.addJoin(tbl("left", table), column, args, true, nil)
}

// RightJoinWhere is JoinWhere as a right join.
//
// Example:
//
//	conn.Select().From("users as u").RightJoinWhere("posts as p", "p.status", "published")
//	// SELECT * FROM "users" AS "u" RIGHT JOIN "posts" AS "p" ON "p"."status" = $1
//	// args: [published]
func (b *query) RightJoinWhere(table, column string, args ...any) *query {
	return b.addJoin(tbl("right", table), column, args, true, nil)
}

// JoinSub joins a subquery: "INNER JOIN (SELECT ...) AS alias ON ...".
//
// Example:
//
//	sub := conn.Select().From("posts").Select("user_id").Where("score", ">", 10)
//	conn.Select().From("users as u").JoinSub(sub, "t", "t.user_id", "u.id")
//	// SELECT * FROM "users" AS "u" INNER JOIN (SELECT "user_id" FROM "posts" WHERE "score" > $1)
//	//     AS "t" ON "t"."user_id" = "u"."id"
//	// args: [10]
func (b *query) JoinSub(sub *query, as, first string, args ...any) *query {
	return b.addJoin(subJoin("inner", sub, as), first, args, false, nil)
}

// LeftJoinSub joins a subquery with a left join.
//
// Example:
//
//	sub := conn.Select().From("posts").Select("user_id").Where("score", ">", 10)
//	conn.Select().From("users as u").LeftJoinSub(sub, "t", "t.user_id", "u.id")
//	// SELECT * FROM "users" AS "u" LEFT JOIN (SELECT "user_id" FROM "posts" WHERE "score" > $1) AS
//	//     "t" ON "t"."user_id" = "u"."id"
//	// args: [10]
func (b *query) LeftJoinSub(sub *query, as, first string, args ...any) *query {
	return b.addJoin(subJoin("left", sub, as), first, args, false, nil)
}

// RightJoinSub joins a subquery with a right join.
//
// Example:
//
//	sub := conn.Select().From("posts").Select("user_id").Where("score", ">", 10)
//	conn.Select().From("users as u").RightJoinSub(sub, "t", "t.user_id", "u.id")
//	// SELECT * FROM "users" AS "u" RIGHT JOIN (SELECT "user_id" FROM "posts" WHERE "score" > $1)
//	//     AS "t" ON "t"."user_id" = "u"."id"
//	// args: [10]
func (b *query) RightJoinSub(sub *query, as, first string, args ...any) *query {
	return b.addJoin(subJoin("right", sub, as), first, args, false, nil)
}

// CrossJoinSub cross joins a subquery.
//
// Example:
//
//	sub := conn.Select().From("roles").Select("name").Where("active", true)
//	conn.Select().From("users").CrossJoinSub(sub, "r")
//	// SELECT * FROM "users" CROSS JOIN (SELECT "name" FROM "roles" WHERE "active" = $1) AS "r"
//	// args: [true]
func (b *query) CrossJoinSub(sub *query, as string) *query {
	return b.addJoin(subJoin("cross", sub, as), "", nil, false, nil)
}

// JoinSubFunc joins a subquery with a complex ON clause built in fn.
//
// Example:
//
//	sub := conn.Select().From("posts").Select("user_id", "score")
//	conn.Select().From("users as u").JoinSubFunc(sub, "p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").Where("p.score", ">", 10)
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN (SELECT "user_id", "score" FROM "posts") AS "p" ON
//	//     "p"."user_id" = "u"."id" AND "p"."score" > $1
//	// args: [10]
func (b *query) JoinSubFunc(sub *query, as string, fn func(*JoinClause)) *query {
	return b.addJoin(subJoin("inner", sub, as), "", nil, false, fn)
}

// LeftJoinSubFunc is JoinSubFunc as a left join.
//
// Example:
//
//	sub := conn.Select().From("posts").Select("user_id", "score")
//	conn.Select().From("users as u").LeftJoinSubFunc(sub, "p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").WhereNotNull("p.score")
//	})
//	// SELECT * FROM "users" AS "u" LEFT JOIN (SELECT "user_id", "score" FROM "posts") AS "p" ON
//	//     "p"."user_id" = "u"."id" AND "p"."score" IS NOT NULL
func (b *query) LeftJoinSubFunc(sub *query, as string, fn func(*JoinClause)) *query {
	return b.addJoin(subJoin("left", sub, as), "", nil, false, fn)
}

// RightJoinSubFunc is JoinSubFunc as a right join.
//
// Example:
//
//	sub := conn.Select().From("posts").Select("user_id", "score")
//	conn.Select().From("users as u").RightJoinSubFunc(sub, "p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id")
//	})
//	// SELECT * FROM "users" AS "u" RIGHT JOIN (SELECT "user_id", "score" FROM "posts") AS "p" ON
//	//     "p"."user_id" = "u"."id"
func (b *query) RightJoinSubFunc(sub *query, as string, fn func(*JoinClause)) *query {
	return b.addJoin(subJoin("right", sub, as), "", nil, false, fn)
}

// JoinLateral adds "INNER JOIN LATERAL (subquery) AS alias ON TRUE" (Postgres, MySQL >= 8.0.14).
//
// Example:
//
//	recent := conn.Select().From("posts").Select("id", "title").
//		WhereColumn("posts.user_id", "u.id").Limit(1)
//	conn.Select().From("users as u").JoinLateral(recent, "rp")
//	// SELECT * FROM "users" AS "u" INNER JOIN LATERAL (SELECT "id", "title" FROM "posts" WHERE
//	//     "posts"."user_id" = "u"."id" LIMIT 1) AS "rp" ON TRUE
//	// MariaDB returns ErrUnsupported
func (b *query) JoinLateral(sub *query, as string) *query {
	j := subJoin("inner", sub, as)
	j.lateral = true
	return b.addJoin(j, "", nil, false, nil)
}

// LeftJoinLateral is JoinLateral as a left join.
//
// Example:
//
//	recent := conn.Select().From("posts").Select("id", "title").
//		WhereColumn("posts.user_id", "u.id").Limit(1)
//	conn.Select().From("users as u").LeftJoinLateral(recent, "rp")
//	// SELECT * FROM "users" AS "u" LEFT JOIN LATERAL (SELECT "id", "title" FROM "posts" WHERE
//	//     "posts"."user_id" = "u"."id" LIMIT 1) AS "rp" ON TRUE
func (b *query) LeftJoinLateral(sub *query, as string) *query {
	j := subJoin("left", sub, as)
	j.lateral = true
	return b.addJoin(j, "", nil, false, nil)
}

// joinKeyword is the SQL for a join type such as "inner" or "left".
func joinKeyword(typ string) string {
	switch typ {
	case "inner":
		return "INNER JOIN "
	case "left":
		return "LEFT JOIN "
	case "right":
		return "RIGHT JOIN "
	case "cross":
		return "CROSS JOIN "
	}
	return strings.ToUpper(typ) + " JOIN "
}

func (c *compiler) join(j joinClause) error {
	if j.typ == "straight_join" && !c.g.SupportsStraightJoin() {
		return fmt.Errorf("%w: straight_join", ErrUnsupported)
	}
	if j.lateral && !c.g.SupportsLateralJoin() {
		return fmt.Errorf("%w: lateral join", ErrUnsupported)
	}
	if j.typ == "straight_join" {
		c.write("STRAIGHT_JOIN ")
	} else {
		c.write(joinKeyword(j.typ))
	}
	if j.lateral {
		c.write("LATERAL ")
	}
	if j.sub != nil {
		if j.alias == "" {
			return fmt.Errorf("%w: subquery join needs an alias", ErrInvalidArgs)
		}
		if err := c.subquery(j.sub); err != nil {
			return err
		}
		c.write(" AS ")
		c.writeTable(j.alias)
	} else {
		c.writeTable(j.table)
	}
	if j.lateral {
		c.write(" ON TRUE")
		return nil
	}
	if len(j.on) == 0 {
		if j.typ == "cross" {
			return nil
		}
		return ErrJoinNeedsOn
	}
	c.write(" ON ")
	return c.clauses(j.on)
}

// OrWhereIn is WhereIn joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OrWhereIn("p.status", []string{"draft"})
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" OR
//	//     "p"."status" IN ($1)
//	// args: [draft]
func (j *JoinClause) OrWhereIn(column string, values any) *JoinClause {
	j.q.OrWhereIn(column, values)
	return j
}

// OrWhereNotIn is WhereNotIn joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OrWhereNotIn("p.status", []string{"spam"})
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" OR
//	//     "p"."status" NOT IN ($1)
//	// args: [spam]
func (j *JoinClause) OrWhereNotIn(column string, values any) *JoinClause {
	j.q.OrWhereNotIn(column, values)
	return j
}

// OrWhereNull is WhereNull joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OrWhereNull("p.deleted_at")
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" OR
//	//     "p"."deleted_at" IS NULL
func (j *JoinClause) OrWhereNull(column string) *JoinClause {
	j.q.OrWhereNull(column)
	return j
}

// OrWhereNotNull is WhereNotNull joined with OR.
//
// Example:
//
//	conn.Select().From("users as u").JoinFunc("posts as p", func(j *db.JoinClause) {
//		j.On("p.user_id", "u.id").OrWhereNotNull("p.published_at")
//	})
//	// SELECT * FROM "users" AS "u" INNER JOIN "posts" AS "p" ON "p"."user_id" = "u"."id" OR
//	//     "p"."published_at" IS NOT NULL
func (j *JoinClause) OrWhereNotNull(column string) *JoinClause {
	j.q.OrWhereNotNull(column)
	return j
}
