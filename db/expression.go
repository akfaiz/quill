package db

// Expression is a raw SQL fragment inlined verbatim into the query.
// Use "?" for bind markers (write "??" for a literal "?"). The SQL text is
// NOT escaped: only pass caller-trusted strings, never user input.
type Expression struct {
	SQL  string
	Args []any
}

// Raw builds an Expression.
func Raw(sql string, args ...any) Expression {
	return Expression{SQL: sql, Args: args}
}
