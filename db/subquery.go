package db

// SelectSub appends "(subquery) as alias" to the select list.
// The subquery is snapshotted: later changes to sub do not affect this query.
func (b *Builder) SelectSub(sub *Builder, as string) *Builder {
	b.columns = append(b.columns, selectItem{sub: sub.Clone(), alias: as})
	return b
}

// FromSub uses "(subquery) as alias" as the source.
func (b *Builder) FromSub(sub *Builder, as string) *Builder {
	b.table, b.fromRaw = "", nil
	b.fromSub, b.fromAlias = sub.Clone(), as
	return b
}

// FromRaw uses a raw SQL fragment as the source (caller-trusted SQL).
func (b *Builder) FromRaw(sql string, args ...any) *Builder {
	e := Raw(sql, args...)
	b.table, b.fromSub = "", nil
	b.fromRaw = &e
	return b
}

// Union appends "union (query)". After a union, OrderBy/Limit/Offset apply to the
// combined result. On SQLite members are wrapped as "select * from (...)".
func (b *Builder) Union(q *Builder) *Builder {
	b.unions = append(b.unions, unionClause{q: q.Clone()})
	return b
}

// UnionAll appends "union all (query)".
func (b *Builder) UnionAll(q *Builder) *Builder {
	b.unions = append(b.unions, unionClause{q: q.Clone(), all: true})
	return b
}
