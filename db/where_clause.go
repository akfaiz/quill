package db

// WhereClause collects the predicates of a parenthesized WHERE group; it only
// offers the where methods. It is passed to WhereGroup/OrWhereGroup callbacks.
type WhereClause struct{ q *query }

// HavingClause collects the predicates of a parenthesized HAVING group; it only
// offers the having methods. It is passed to HavingGroup/OrHavingGroup callbacks.
type HavingClause struct{ q *query }
