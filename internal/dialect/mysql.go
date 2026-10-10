package dialect

import "strconv"

// MySQL returns the MySQL grammar.
func MySQL() Grammar { return mysqlGrammar{} }

// mysqlGrammar is the MySQL grammar. MariaDB embeds it.
type mysqlGrammar struct{}

func (mysqlGrammar) LikeOp(cs, not bool) string {
	op := "like"
	if cs {
		op = "like binary"
	}
	if not {
		op = "not " + op
	}
	return op
}
func (mysqlGrammar) LikeValue(p string, _ bool) string { return p }
func (mysqlGrammar) LikeCast(string) string            { return "" }
func (mysqlGrammar) NullSafeOp() string                { return "<=>" }
func (mysqlGrammar) DatePart(part, col string) string  { return part + "(" + col + ")" }
func (mysqlGrammar) DateValueWrap() (string, string)   { return "", "" }
func (mysqlGrammar) SupportsStraightJoin() bool        { return true }

// SupportsLateralJoin: MySQL >= 8.0.14 only; older servers will reject the SQL.
func (mysqlGrammar) SupportsLateralJoin() bool { return true }
func (mysqlGrammar) UnionOpen() string         { return "(" }
func (mysqlGrammar) LockSQL(k LockKind, raw string) string {
	switch k {
	case LockUpdate:
		return "for update"
	case LockShared:
		return "lock in share mode"
	case LockRaw:
		return raw
	}
	return ""
}
func (mysqlGrammar) IndexHint(kind, index string) (string, error) {
	if err := validateIndexNames(index, true); err != nil {
		return "", err
	}
	return kind + " index (" + index + ")", nil
}
func (mysqlGrammar) Random(seed *int) string {
	if seed == nil {
		return "RAND()"
	}
	return "RAND(" + strconv.Itoa(*seed) + ")"
}

func (mysqlGrammar) Quote(s string) string  { return "`" + doubleChar(s, '`') + "`" }
func (mysqlGrammar) Placeholder(int) string { return "?" }
func (mysqlGrammar) UnboundedLimit() string { return "18446744073709551615" }
