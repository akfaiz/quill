// Package db is a fluent SQL query builder for Go modelled on Laravel's
// query builder (Illuminate\Database\Query). It builds SQL and bind arguments
// and, optionally, runs them through database/sql. It is not an ORM: there are
// no models, relations, hooks or migrations.
//
// Build only (set the dialect first, then chain from Table):
//
//	sql, args, err := db.Use(db.Postgres).Table("users").
//		Where("votes", ">", 100).
//		OrderByDesc("created_at").
//		Limit(10).
//		ToSQL()
//
// Build and execute:
//
//	conn := db.New(sqlDB, db.Postgres)
//	var users []User
//	err := conn.Table("users").Where("active", true).Get(ctx, &users)
//
// Typed queries take the table from the row type's TableName method:
//
//	users, err := db.From[User](conn).Where("active", true).Get(ctx)
//	id, err := db.From[User](conn).InsertGetID(ctx, User{Name: "Ann"})
//
// Values are always sent as bind arguments. Identifiers are always quoted.
// The *Raw methods and Raw expressions take caller-trusted SQL: never build them
// from user input.
package db
