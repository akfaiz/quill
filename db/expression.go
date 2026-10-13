package db

// Expression is a raw SQL fragment inlined verbatim into the query.
// Use "?" for bind markers (write "??" for a literal "?"). The SQL text is
// NOT escaped: only pass caller-trusted strings, never user input.
type Expression struct {
	SQL  string
	Args []any
}

// Raw builds an Expression.
//
// Example:
//
//	conn.Select().From("users").Where("created_at", ">", db.Raw("now() - interval '1 day'"))
//	// SELECT * FROM "users" WHERE "created_at" > now() - interval '1 day'
func Raw(sql string, args ...any) Expression {
	return Expression{SQL: sql, Args: args}
}
