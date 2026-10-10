package db

// Latest orders by column descending (default "created_at").
func (b *Builder) Latest(column ...string) *Builder { return b.OrderBy(orDefault(column), "desc") }

// Oldest orders by column ascending (default "created_at").
func (b *Builder) Oldest(column ...string) *Builder { return b.OrderBy(orDefault(column), "asc") }

func orDefault(column []string) string {
	if len(column) > 0 && column[0] != "" {
		return column[0]
	}
	return "created_at"
}

// InRandomOrder orders randomly: RANDOM() on Postgres/SQLite, RAND([seed]) on MySQL.
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
func (b *Builder) Reorder() *Builder {
	b.orders, b.unionOrders = nil, nil
	return b
}

// ReorderBy is Reorder followed by OrderBy(column, direction).
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

// ForPageBeforeID pages backwards by keyset: "id < lastID order by id desc limit n".
// A nil lastID selects "id is not null". column defaults to "id".
func (b *Builder) ForPageBeforeID(perPage int, lastID any, column ...string) *Builder {
	col := idColumn(column)
	b.removeOrdersFor(col)
	if lastID == nil {
		b.WhereNotNull(col)
	} else {
		b.Where(col, "<", lastID)
	}
	return b.OrderByDesc(col).Limit(perPage)
}

// ForPageAfterID pages forwards by keyset: "id > lastID order by id asc limit n".
func (b *Builder) ForPageAfterID(perPage int, lastID any, column ...string) *Builder {
	col := idColumn(column)
	b.removeOrdersFor(col)
	if lastID == nil {
		b.WhereNotNull(col)
	} else {
		b.Where(col, ">", lastID)
	}
	return b.OrderBy(col, "asc").Limit(perPage)
}

func idColumn(column []string) string {
	if len(column) > 0 && column[0] != "" {
		return column[0]
	}
	return "id"
}
