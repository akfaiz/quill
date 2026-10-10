package db

import (
	"regexp"
	"strings"

	"github.com/akfaiz/quill/internal/jsonpath"

	"github.com/akfaiz/quill/internal/dialect"
)

var aliasRe = regexp.MustCompile(`(?i)^(.+?)\s+as\s+(.+)$`)

// compiler accumulates SQL text and bind arguments in order, so placeholder
// numbering stays correct across nested subqueries.
type compiler struct {
	d      Dialect
	g      dialect.Grammar
	prefix string
	sb     strings.Builder
	args   []any
}

func (c *compiler) write(s string) { c.sb.WriteString(s) }

// param appends a bind argument and writes its placeholder.
func (c *compiler) param(v any) {
	c.args = append(c.args, v)
	c.write(c.g.Placeholder(len(c.args)))
}

// raw writes an Expression, translating "?" markers into dialect placeholders.
// Markers inside quoted strings/identifiers are left alone; "??" is a literal "?".
func (c *compiler) raw(e Expression) {
	argi := 0
	var quote byte
	s := e.SQL
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case quote != 0:
			c.sb.WriteByte(ch)
			if ch == quote {
				quote = 0
			}
		case ch == '\'' || ch == '"' || ch == '`':
			quote = ch
			c.sb.WriteByte(ch)
		case ch == '?' && i+1 < len(s) && s[i+1] == '?':
			c.sb.WriteByte('?')
			i++
		case ch == '?':
			if argi < len(e.Args) {
				c.param(e.Args[argi])
				argi++
			} else {
				c.sb.WriteByte('?')
			}
		default:
			c.sb.WriteByte(ch)
		}
	}
}

// wrapColumn quotes a column reference: "t.col", "col as alias", "*", "t.*".
func (c *compiler) wrapColumn(s string) string {
	if m := aliasRe.FindStringSubmatch(s); m != nil {
		return c.wrapColumn(m[1]) + " as " + c.g.Quote(m[2])
	}
	if jsonpath.IsSelector(s) {
		return c.wrapJSONSelector(s)
	}
	segs := strings.Split(s, ".")
	for i, seg := range segs {
		switch {
		case seg == "*":
		case i == 0 && len(segs) > 1:
			segs[i] = c.g.Quote(c.prefix + seg)
		default:
			segs[i] = c.g.Quote(seg)
		}
	}
	return strings.Join(segs, ".")
}

// wrapTable quotes a table reference ("schema.tbl", "tbl as t"), applying the
// table prefix to the table name and alias.
func (c *compiler) wrapTable(s string) string {
	if m := aliasRe.FindStringSubmatch(s); m != nil {
		return c.wrapTable(m[1]) + " as " + c.g.Quote(c.prefix+m[2])
	}
	segs := strings.Split(s, ".")
	for i, seg := range segs {
		if i == len(segs)-1 {
			seg = c.prefix + seg
		}
		segs[i] = c.g.Quote(seg)
	}
	return strings.Join(segs, ".")
}

// bareColumn strips a table qualifier and alias: "t.id", "id as x" -> "id" / "x".
func bareColumn(c string) string {
	if m := aliasRe.FindStringSubmatch(c); m != nil {
		return strings.TrimSpace(m[2])
	}
	if i := strings.LastIndex(c, "."); i >= 0 {
		return c[i+1:]
	}
	return c
}
