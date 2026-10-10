// Package errs holds error values shared between the root package and the
// internal packages, which cannot import the root without a cycle. The root
// re-exports them, so errors.Is works on either name.
package errs

import "errors"

// ErrInvalidArgs reports arguments the library cannot use.
var ErrInvalidArgs = errors.New("quill: invalid arguments")
