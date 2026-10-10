// Package rawsql interpolates bind arguments into SQL text for debugging.
// The result is NOT injection-safe: never execute or log it in production.
package rawsql

import (
	"fmt"
	"strings"
	"time"
)

// Literal renders v as a SQL literal (strings and bytes quoted with ' doubled).
func Literal(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		return "'" + strings.ReplaceAll(x, "'", "''") + "'"
	case []byte:
		return "'" + strings.ReplaceAll(string(x), "'", "''") + "'"
	case time.Time:
		return "'" + x.Format("2006-01-02 15:04:05") + "'"
	}
	return fmt.Sprint(v)
}

// Interpolate substitutes args for the placeholders in sql: "$1", "$2", ... when
// numbered, otherwise each "?" in order. Text inside '…', "…" and `…` is left
// alone, so a literal such as '?' or '$1' is never mistaken for a placeholder.
// It is best effort: backslash escapes and dollar-quoted strings are not parsed.
func Interpolate(sql string, args []any, numbered bool) string {
	var sb strings.Builder
	var quote byte
	argi := 0
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		switch {
		case quote != 0:
			sb.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
			sb.WriteByte(ch)
		case !numbered && ch == '?' && argi < len(args):
			sb.WriteString(Literal(args[argi]))
			argi++
		case numbered && ch == '$':
			j, n := i+1, 0
			for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
				n = n*10 + int(sql[j]-'0')
				j++
			}
			if j > i+1 && n >= 1 && n <= len(args) {
				sb.WriteString(Literal(args[n-1])) //nolint:gosec // n is bounded to 1..len(args) above.
				i = j - 1
				continue
			}
			sb.WriteByte(ch)
		default:
			sb.WriteByte(ch)
		}
	}
	return sb.String()
}
