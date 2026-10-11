package db

import (
	"fmt"
	"reflect"
	"sync"
)

// Tabler is implemented by row types that know their table, which lets From[T]
// work without a table name:
//
//	func (User) TableName() string { return "users" }
//
// Either receiver kind works, and the method is promoted through embedding.
//
// The method is called on a zero value, once per type: the result is cached, so a
// name that changes later is not picked up. A method that panics on a zero value
// (for example by dereferencing a nil embedded pointer) is reported as
// ErrInvalidArgs instead. For per-tenant or per-period tables, give the name
// explicitly with As[T](conn.Select().From(name)).
type Tabler interface{ TableName() string }

var tableNames sync.Map // reflect.Type -> string

// TableName returns T's table from its TableName method. T may be a pointer type.
// It returns ErrInvalidArgs when T has no TableName method or it returns "".
//
// Example:
//
//	type Order struct{ ID int64 }
//	func (Order) TableName() string { return "orders" }
//
//	name, err := db.TableName[Order]()   // name = "orders"
//	_, err = db.TableName[struct{}]()    // err wraps ErrInvalidArgs (no TableName method)
func TableName[T any]() (string, error) {
	t := reflect.TypeFor[T]()
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if name, ok := tableNames.Load(t); ok {
		return name.(string), nil
	}
	// *T carries the methods of both receiver kinds
	tb, ok := reflect.TypeAssert[Tabler](reflect.New(t))
	if !ok {
		return "", fmt.Errorf("%w: %s has no TableName() string method", ErrInvalidArgs, t)
	}
	name, err := callTableName(tb, t)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf("%w: %s.TableName() returned an empty name", ErrInvalidArgs, t)
	}
	tableNames.Store(t, name)
	return name, nil
}

// callTableName turns a panic in the user's method into an error.
func callTableName(tb Tabler, t reflect.Type) (name string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%w: %s.TableName() panicked on a zero value (%v); it must not depend on field values",
				ErrInvalidArgs, t, r)
		}
	}()
	return tb.TableName(), nil
}

// From starts a typed query on T's table: db.From[User](conn).Where(...).Get(ctx).
// A problem with T (no TableName method) is reported by the first terminal
// call, like any other builder error, so the chain stays unbroken.
//
// Example:
//
//	q := db.From[Order](conn) // same as db.As[Order](conn.Select().From("orders"))
func From[T any](db *DB) *Query[T] {
	if db == nil {
		return As[T](&SelectBuilder{q: query{err: ErrNotBound}})
	}
	s := db.Select()
	name, err := TableName[T]()
	if err != nil {
		s.q.err = err
		return As[T](s)
	}
	return As[T](s.From(name))
}
