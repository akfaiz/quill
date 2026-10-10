package dialect

import (
	"strings"
)

// SQLite returns the SQLite grammar.
func SQLite() Grammar { return sqliteGrammar{} }

// sqliteGrammar is the SQLite grammar.
type sqliteGrammar struct{}

func (sqliteGrammar) LikeOp(cs, not bool) string {
	op := "like"
	if cs {
		op = "glob"
	}
	if not {
		op = "not " + op
	}
	return op
}
func (sqliteGrammar) LikeValue(p string, cs bool) string {
	if !cs {
		return p
	}
	return strings.NewReplacer("*", "[*]", "?", "[?]", "%", "*", "_", "?").Replace(p)
}
func (sqliteGrammar) LikeCast(string) string { return "" }
func (sqliteGrammar) NullSafeOp() string     { return "is" }

var sqliteDateFormats = map[string]string{
	"date": "%Y-%m-%d", "time": "%H:%M:%S", "day": "%d", "month": "%m", "year": "%Y",
}

func (sqliteGrammar) DatePart(part, col string) string {
	return "strftime('" + sqliteDateFormats[part] + "', " + col + ")"
}
func (sqliteGrammar) DateValueWrap() (string, string) { return "cast(", " as text)" }
func (sqliteGrammar) SupportsStraightJoin() bool      { return false }
func (sqliteGrammar) SupportsLateralJoin() bool       { return false }
func (sqliteGrammar) UnionOpen() string               { return "select * from (" }
func (sqliteGrammar) LockSQL(LockKind, string) string { return "" }
func (sqliteGrammar) IndexHint(kind, index string) (string, error) {
	if kind != "force" {
		return "", nil
	}
	if err := validateIndexNames(index, false); err != nil {
		return "", err
	}
	return "indexed by " + index, nil
}
func (sqliteGrammar) Random(*int) string { return "RANDOM()" }

func (sqliteGrammar) Quote(s string) string  { return `"` + doubleChar(s, '"') + `"` }
func (sqliteGrammar) QuoteChar() byte        { return '"' }
func (sqliteGrammar) Placeholder(int) string { return "?" }
func (sqliteGrammar) UnboundedLimit() string { return "-1" }
