package db

import (
	"errors"

	"github.com/akfaiz/quill/internal/errs"
)

// Sentinel errors returned by the builder. Match them with errors.Is: most are wrapped
// with a message that names the offending argument.
var (
	// ErrUnknownDialect is returned when the Dialect is not one of the supported values.
	ErrUnknownDialect = errors.New("quill: unknown dialect")
	// ErrInvalidOperator is returned for a comparison operator outside the allow-list.
	ErrInvalidOperator = errors.New("quill: invalid operator")
	// ErrInvalidDirection is returned when an order direction is not "asc" or "desc".
	ErrInvalidDirection = errors.New("quill: order direction must be asc or desc")
	// ErrNoTable is returned when a query is built without a table or subquery source.
	ErrNoTable = errors.New("quill: no table specified")
	// ErrInvalidArgs is returned for arguments the library cannot use, such as a wrong
	// argument count, a non-struct passed to a struct write or a malformed cursor.
	ErrInvalidArgs = errs.ErrInvalidArgs
	// ErrUnsupported is returned when the dialect cannot express the feature, instead of
	// emitting SQL that behaves differently.
	ErrUnsupported = errors.New("quill: not supported by this dialect")
	// ErrJoinNeedsOn is returned for a join (other than a cross join) without an ON condition.
	ErrJoinNeedsOn = errors.New("quill: join requires an ON condition")
	// ErrNoValues is returned by a write that has no values to write.
	ErrNoValues = errors.New("quill: no values to write")
	// ErrNoWhere is returned by Update, Delete and Increment* without a WHERE or LIMIT
	// unless AllRows was called.
	ErrNoWhere = errors.New("quill: update/delete without a where clause; call AllRows() to confirm")
)
