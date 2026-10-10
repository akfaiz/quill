// Package dialect holds the SQL differences between databases behind one
// interface: identifier quoting, placeholders, LIKE and date handling, locks,
// index hints and a few join/union rules. Everything the databases share lives in
// the compiler of the root package, so supporting a new database is a new Grammar.
package dialect

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/akfaiz/quill/internal/errs"
)

// LockKind selects a row-locking clause.
type LockKind int

const (
	LockNone LockKind = iota
	LockUpdate
	LockShared
	LockRaw
)

// Grammar holds the per-dialect differences. Everything shared lives in the compiler.
type Grammar interface {
	// Quote wraps a single identifier segment, escaping embedded quote chars.
	Quote(segment string) string
	// QuoteChar is the identifier quote byte Quote wraps with, so callers can write a
	// quoted identifier straight into a buffer without allocating.
	QuoteChar() byte
	// Placeholder returns the bind marker for the n-th (1-based) argument.
	Placeholder(n int) string
	// UnboundedLimit returns the limit literal required when OFFSET is used
	// without LIMIT, or "" when the dialect accepts a bare OFFSET.
	UnboundedLimit() string
	// LikeOp returns the LIKE operator for the case-sensitivity / negation combo.
	LikeOp(caseSensitive, not bool) string
	// LikeValue adapts a LIKE pattern for the chosen operator.
	LikeValue(pattern string, caseSensitive bool) string
	// LikeCast returns a suffix appended to the wrapped column for LIKE operators.
	LikeCast(op string) string
	// NullSafeOp returns the NULL-safe equality operator.
	NullSafeOp() string
	// DatePart extracts part ("date","time","day","month","year") from a wrapped column.
	DatePart(part, col string) string
	// DateValueWrap wraps the right-hand side of a date comparison.
	DateValueWrap() (pre, post string)
	SupportsStraightJoin() bool
	SupportsLateralJoin() bool
	// UnionOpen opens a wrapped union member: "(" or "select * from (".
	UnionOpen() string
	// LockSQL returns the locking clause, or "" when none applies.
	LockSQL(kind LockKind, raw string) string
	// IndexHint renders an index hint after the FROM table, or "" when unsupported.
	IndexHint(kind, index string) (string, error)
	// Random returns the random-order expression; seed may be nil.
	Random(seed *int) string
}

var indexNameRe = regexp.MustCompile(`^[a-zA-Z0-9_$]+$`)

// validateIndexNames guards index names (they are emitted unquoted).
func validateIndexNames(index string, list bool) error {
	names := []string{index}
	if list {
		names = strings.Split(index, ",")
	}
	for _, n := range names {
		if !indexNameRe.MatchString(strings.TrimSpace(n)) {
			return fmt.Errorf("%w: index name %q contains invalid characters", errs.ErrInvalidArgs, n)
		}
	}
	return nil
}

func doubleChar(s string, c byte) string {
	if strings.IndexByte(s, c) < 0 { // nothing to escape: no copy
		return s
	}
	out := make([]byte, 0, len(s)+1)
	for i := range len(s) {
		if s[i] == c {
			out = append(out, c)
		}
		out = append(out, s[i])
	}
	return string(out)
}
