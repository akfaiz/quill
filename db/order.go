package db

import "fmt"

// Latest orders by column descending (default "created_at").
//
// Example:
//
//	conn.Table("posts").Latest("published_at")
//	// SELECT * FROM "posts" ORDER BY "published_at" DESC
func (b *Builder) Latest(column ...string) *Builder { return b.OrderBy(orDefault(column), "desc") }

// Oldest orders by column ascending (default "created_at").
//
// Example:
//
//	conn.Table("posts").Oldest()
//	// SELECT * FROM "posts" ORDER BY "created_at" ASC
func (b *Builder) Oldest(column ...string) *Builder { return b.OrderBy(orDefault(column), "asc") }

func orDefault(column []string) string {
	if len(column) > 0 && column[0] != "" {
		return column[0]
	}
	return "created_at"
}

// InRandomOrder orders randomly: RANDOM() on Postgres/SQLite, RAND([seed]) on MySQL.
//
// Example:
//
//	conn.Table("posts").InRandomOrder().Limit(5)
//	// Postgres: SELECT * FROM "posts" ORDER BY RANDOM() LIMIT 5
//	// MySQL: SELECT * FROM `posts` ORDER BY RAND() LIMIT 5
func (b *Builder) InRandomOrder(seed ...int) *Builder {
	var s *int
	if len(seed) > 0 {
		s = &seed[0]
	}
	g, err := b.dialect.grammar()
	if err != nil {
		return b.fail(err)
	}
	return b.OrderByRaw(g.Random(s))
}

// Reorder removes all existing ORDER BY clauses (including a union's).
//
// Example:
//
//	conn.Table("posts").OrderBy("title").Reorder()
//	// SELECT * FROM "posts"
func (b *Builder) Reorder() *Builder {
	b.orders, b.unionOrders = nil, nil
	return b
}

// ReorderBy is Reorder followed by OrderBy(column, direction).
//
// Example:
//
//	conn.Table("posts").OrderBy("title").ReorderBy("id", "desc")
//	// SELECT * FROM "posts" ORDER BY "id" DESC
func (b *Builder) ReorderBy(column string, direction ...string) *Builder {
	return b.Reorder().OrderBy(column, direction...)
}

func (b *Builder) removeOrdersFor(column string) {
	kept := b.orders[:0:0]
	for _, o := range b.orders {
		if o.raw != nil || o.column != column {
			kept = append(kept, o)
		}
	}
	b.orders = kept
}

// ForPageBeforeID pages backwards by keyset: "id < lastID ORDER BY id DESC LIMIT n".
// A nil lastID selects "id IS NOT NULL". column defaults to "id".
//
// Example:
//
//	conn.Table("posts").ForPageBeforeID(10, 42)
//	// SELECT * FROM "posts" WHERE "id" < $1 ORDER BY "id" DESC LIMIT 10   args: [42]
func (b *Builder) ForPageBeforeID(perPage int, lastID any, column ...string) *Builder {
	if len(b.unions) > 0 {
		return b.fail(errKeysetUnion)
	}
	col := idColumn(column)
	b.removeOrdersFor(col)
	b.wheres = isolated(b.wheres)
	if lastID == nil {
		b.WhereNotNull(col)
	} else {
		b.Where(col, "<", lastID)
	}
	return b.OrderByDesc(col).Limit(perPage)
}

// ForPageAfterID pages forwards by keyset: "id > lastID ORDER BY id ASC LIMIT n".
//
// Example:
//
//	conn.Table("posts").ForPageAfterID(10, 42)
//	// SELECT * FROM "posts" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 10   args: [42]
func (b *Builder) ForPageAfterID(perPage int, lastID any, column ...string) *Builder {
	if len(b.unions) > 0 {
		return b.fail(errKeysetUnion)
	}
	col := idColumn(column)
	b.removeOrdersFor(col)
	b.wheres = isolated(b.wheres)
	if lastID == nil {
		b.WhereNotNull(col)
	} else {
		b.Where(col, ">", lastID)
	}
	return b.OrderBy(col, "asc").Limit(perPage)
}

// errKeysetUnion is recorded when keyset paging meets a union: the key condition
// would land in the first member only, so every page would re-read the others.
var errKeysetUnion = fmt.Errorf(
	"%w: keyset paging on a union; wrap it with FromSub and page the wrapper", ErrInvalidArgs)

func idColumn(column []string) string {
	if len(column) > 0 && column[0] != "" {
		return column[0]
	}
	return "id"
}
