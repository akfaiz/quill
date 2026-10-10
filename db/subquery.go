package db

// SelectSub appends "(subquery) as alias" to the select list.
// The subquery is snapshotted: later changes to sub do not affect this query.
//
// Example:
//
//	n := conn.Table("posts").SelectRaw("count(*)").WhereColumn("posts.user_id", "users.id")
//	conn.Table("users").Select("name").SelectSub(n, "post_count")
//	// SELECT "name", (SELECT count(*) FROM "posts" WHERE "posts"."user_id" = "users"."id") AS
//	//     "post_count" FROM "users"
func (b *Builder) SelectSub(sub *Builder, as string) *Builder {
	if sub == nil {
		return b.fail(errNilSubquery)
	}
	b.columns = append(b.columns, selectItem{sub: sub.Clone(), alias: as})
	return b
}

// FromSub uses "(subquery) as alias" as the source.
//
// Example:
//
//	top := conn.Table("orders").SelectRaw("user_id, sum(total) as spent").GroupBy("user_id")
//	conn.Table("").FromSub(top, "t").Where("spent", ">", 100)
//	// SELECT * FROM (SELECT user_id, sum(total) as spent FROM "orders" GROUP BY "user_id") AS
//	//     "t" WHERE "spent" > $1
//	//     args: [100]
func (b *Builder) FromSub(sub *Builder, as string) *Builder {
	if sub == nil {
		return b.fail(errNilSubquery)
	}
	b.table, b.fromRaw = "", nil
	b.fromSub, b.fromAlias = sub.Clone(), as
	return b
}

// FromRaw uses a raw SQL fragment as the source (caller-trusted SQL).
//
// Example:
//
//	conn.Table("").FromRaw("generate_series(?, ?) as g", 1, 3)
//	// SELECT * FROM generate_series($1, $2) as g   args: [1 3]
func (b *Builder) FromRaw(sql string, args ...any) *Builder {
	e := Raw(sql, args...)
	b.table, b.fromSub = "", nil
	b.fromRaw = &e
	return b
}

// Union appends "UNION (query)". After a union, OrderBy/Limit/Offset apply to the
// combined result. On SQLite members are wrapped as "SELECT * FROM (...)".
//
// Example:
//
//	a := conn.Table("users").Select("name").Where("active", true)
//	b := conn.Table("admins").Select("name")
//	a.Union(b).OrderBy("name").Limit(10)
//	// (SELECT "name" FROM "users" WHERE "active" = $1) UNION (SELECT "name" FROM "admins") ORDER
//	//     BY "name" ASC LIMIT 10
//	//     args: [true]
func (b *Builder) Union(q *Builder) *Builder {
	if q == nil {
		return b.fail(errNilSubquery)
	}
	b.unions = append(b.unions, unionClause{q: q.Clone()})
	return b
}

// UnionAll appends "UNION ALL (query)".
//
// Example:
//
//	a := conn.Table("users").Select("name").Where("active", true)
//	b := conn.Table("admins").Select("name")
//	a.UnionAll(b).OrderBy("name").Limit(10)
//	// (SELECT "name" FROM "users" WHERE "active" = $1) UNION ALL (SELECT "name" FROM "admins")
//	//     ORDER BY "name" ASC LIMIT 10
//	//     args: [true]
func (b *Builder) UnionAll(q *Builder) *Builder {
	if q == nil {
		return b.fail(errNilSubquery)
	}
	b.unions = append(b.unions, unionClause{q: q.Clone(), all: true})
	return b
}
