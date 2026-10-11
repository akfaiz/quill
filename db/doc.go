// Package db is a fluent SQL query builder for Go. It builds SQL and bind arguments
// and, optionally, runs them through database/sql. It is not an ORM: there are
// no models, relations, hooks or migrations.
//
// Build only (set the dialect first, then chain from Select, Insert, Update or Delete):
//
//	sql, args, err := db.Use(db.Postgres).Select().From("users").
//		Where("votes", ">", 100).
//		OrderByDesc("created_at").
//		Limit(10).
//		ToSQL()
//
// Build and execute:
//
//	conn := db.New(sqlDB, db.Postgres)
//	var users []User
//	err := conn.Select().From("users").Where("active", true).Get(ctx, &users)
//
// Typed reads take the table from the row type's TableName method (writes use
// the Insert, Update and Delete builders):
//
//	users, err := db.From[User](conn).Where("active", true).Get(ctx)
//
// Values are always sent as bind arguments. Identifiers are always quoted.
// The *Raw methods and Raw expressions take caller-trusted SQL: never build them
// from user input.
package db

//go:generate go run ../internal/chaingen/cmd .
