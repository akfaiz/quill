package db

// SelectBuilder is a mutable, fluent SELECT query. Not safe for concurrent use;
// call Clone to branch a query. Start one with DB.Select.
//
// Most chain methods are generated into chain_gen.go from the query type; the
// ones below take or return a SelectBuilder themselves.
type SelectBuilder struct{ q query }

// sq unwraps a public subquery; a nil builder stays nil so the usual nil
// subquery error is reported.
func sq(s *SelectBuilder) *query {
	if s == nil {
		return nil
	}
	return &s.q
}

// subOf reports whether v is a subquery builder and unwraps it.
func subOf(v any) (*query, bool) {
	switch x := v.(type) {
	case *query:
		return x, true
	case *SelectBuilder:
		return sq(x), true
	}
	return nil, false
}

// Clone returns an independent copy. Cloning a nil *SelectBuilder returns nil.
//
// Example:
//
//	base := conn.Select().From("users").Where("active", true)
//	admins := base.Clone().Where("role", "admin")
//	// base:   SELECT * FROM "users" WHERE "active" = $1
//	// admins: SELECT * FROM "users" WHERE "active" = $1 AND "role" = $2
//	// args: [true admin] (admins)
func (s *SelectBuilder) Clone() *SelectBuilder {
	if s == nil {
		return nil
	}
	return &SelectBuilder{q: *s.q.Clone()}
}

// When applies fn when cond is true.
//
// Example:
//
//	conn.Select().From("users").When(onlyActive, func(q *db.SelectBuilder) {
//		q.Where("active", true)
//	})
//	// onlyActive == true: SELECT * FROM "users" WHERE "active" = $1
func (s *SelectBuilder) When(cond bool, fn func(*SelectBuilder)) *SelectBuilder {
	if cond && fn != nil {
		fn(s)
	}
	return s
}

// Unless applies fn when cond is false.
//
// Example:
//
//	conn.Select().From("users").Unless(includeBanned, func(q *db.SelectBuilder) {
//		q.Where("banned", false)
//	})
//	// includeBanned == false: SELECT * FROM "users" WHERE "banned" = $1
func (s *SelectBuilder) Unless(cond bool, fn func(*SelectBuilder)) *SelectBuilder {
	return s.When(!cond, fn)
}

// Tap runs fn for side effects and returns the builder.
//
// Example:
//
//	conn.Select().From("users").Tap(func(q *db.SelectBuilder) { q.Where("id", 1) })
//	// SELECT * FROM "users" WHERE "id" = $1
func (s *SelectBuilder) Tap(fn func(*SelectBuilder)) *SelectBuilder {
	if fn != nil {
		fn(s)
	}
	return s
}
