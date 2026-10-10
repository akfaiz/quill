package dialect

import (
	"strconv"
	"strings"
)

// Postgres returns the PostgreSQL grammar.
func Postgres() Grammar { return pgGrammar{} }

// pgGrammar is the PostgreSQL grammar.
type pgGrammar struct{}

func (pgGrammar) LikeOp(cs, not bool) string {
	op := "ilike"
	if cs {
		op = "like"
	}
	if not {
		op = "not " + op
	}
	return op
}
func (pgGrammar) LikeValue(p string, _ bool) string { return p }
func (pgGrammar) LikeCast(op string) string {
	if strings.Contains(op, "like") {
		return "::text"
	}
	return ""
}
func (pgGrammar) NullSafeOp() string { return "is not distinct from" }
func (pgGrammar) DatePart(part, col string) string {
	switch part {
	case "date", "time":
		return col + "::" + part
	}
	return "extract(" + part + " from " + col + ")"
}
func (pgGrammar) DateValueWrap() (string, string) { return "", "" }
func (pgGrammar) SupportsStraightJoin() bool      { return false }
func (pgGrammar) SupportsLateralJoin() bool       { return true }
func (pgGrammar) UnionOpen() string               { return "(" }
func (pgGrammar) LockSQL(k LockKind, raw string) string {
	switch k {
	case LockUpdate:
		return "for update"
	case LockShared:
		return "for share"
	case LockRaw:
		return raw
	}
	return ""
}
func (pgGrammar) IndexHint(string, string) (string, error) { return "", nil }
func (pgGrammar) Random(*int) string                       { return "RANDOM()" }

func (pgGrammar) Quote(s string) string { return `"` + doubleChar(s, '"') + `"` }
func (pgGrammar) QuoteChar() byte       { return '"' }
func (pgGrammar) Placeholder(n int) string {
	if n >= 0 && n < len(pgPlaceholders) {
		return pgPlaceholders[n]
	}
	return "$" + strconv.Itoa(n)
}

// pgPlaceholders caches "$0".."$99" so typical queries allocate no placeholder strings.
var pgPlaceholders = func() (p [100]string) {
	for i := range p {
		p[i] = "$" + strconv.Itoa(i)
	}
	return
}()

func (pgGrammar) UnboundedLimit() string { return "" }
