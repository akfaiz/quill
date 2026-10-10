package db

import (
	"errors"

	"github.com/akfaiz/quill/internal/errs"
)

var (
	ErrUnknownDialect   = errors.New("quill: unknown dialect")
	ErrInvalidOperator  = errors.New("quill: invalid operator")
	ErrInvalidDirection = errors.New("quill: order direction must be asc or desc")
	ErrNoTable          = errors.New("quill: no table specified")
	ErrInvalidArgs      = errs.ErrInvalidArgs
	ErrUnsupported      = errors.New("quill: not supported by this dialect")
	ErrJoinNeedsOn      = errors.New("quill: join requires an ON condition")
	ErrNoValues         = errors.New("quill: no values to write")
	ErrNoWhere          = errors.New("quill: update/delete without a where clause; call AllRows() to confirm")
)
