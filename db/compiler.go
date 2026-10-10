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

// reserve sizes the output buffer and the args slice from the root query's shape,
// so a typical query grows each at most once instead of doubling repeatedly.
// The numbers are estimates: undershooting only costs a regrow, never correctness.
func (c *compiler) reserve(b *Builder) {
	onCount := 0
	for _, j := range b.joins {
		onCount += len(j.on)
	}
	parts := len(b.columns) + len(b.joins) + onCount + len(b.wheres) + len(b.groups) +
		len(b.havings) + len(b.orders) + 2*len(b.ctes) + 4*len(b.unions)
	c.sb.Grow(64 + 24*parts)
	if n := onCount + len(b.wheres) + len(b.havings); n > 0 {
		c.args = make([]any, 0, n)
	}
}

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

// quoteTo writes one identifier segment, quoted and with embedded quote chars
// doubled, without allocating (see Grammar.QuoteChar).
func (c *compiler) quoteTo(sb *strings.Builder, seg string) {
	q := c.g.QuoteChar()
	sb.WriteByte(q)
	for {
		i := strings.IndexByte(seg, q)
		if i < 0 {
			break
		}
		sb.WriteString(seg[:i+1])
		sb.WriteByte(q)
		seg = seg[i+1:]
	}
	sb.WriteString(seg)
	sb.WriteByte(q)
}

// wrapColumn quotes a column reference: "t.col", "col as alias", "*", "t.*".
func (c *compiler) wrapColumn(s string) string {
	var sb strings.Builder
	c.columnTo(&sb, s)
	return sb.String()
}

// writeColumn is wrapColumn written straight into the output buffer.
func (c *compiler) writeColumn(s string) { c.columnTo(&c.sb, s) }

func (c *compiler) columnTo(sb *strings.Builder, s string) {
	if name, alias, ok := matchAlias(s); ok {
		c.columnTo(sb, name)
		sb.WriteString(" AS ")
		c.quoteTo(sb, alias)
		return
	}
	if jsonpath.IsSelector(s) {
		sb.WriteString(c.wrapJSONSelector(s))
		return
	}
	for i := 0; ; i++ {
		seg, rest, more := strings.Cut(s, ".")
		switch {
		case seg == "*":
			sb.WriteString(seg)
		case i == 0 && more:
			c.quoteTo(sb, c.prefix+seg) // a qualifier is a table name or alias: prefixed
		default:
			c.quoteTo(sb, seg)
		}
		if !more {
			return
		}
		sb.WriteByte('.')
		s = rest
	}
}

// wrapTable quotes a table reference ("schema.tbl", "tbl as t"), applying the
// table prefix to the table name and alias.
func (c *compiler) wrapTable(s string) string {
	var sb strings.Builder
	c.tableTo(&sb, s)
	return sb.String()
}

// writeTable is wrapTable written straight into the output buffer.
func (c *compiler) writeTable(s string) { c.tableTo(&c.sb, s) }

func (c *compiler) tableTo(sb *strings.Builder, s string) {
	if name, alias, ok := matchAlias(s); ok {
		c.tableTo(sb, name)
		sb.WriteString(" AS ")
		c.quoteTo(sb, c.prefix+alias)
		return
	}
	for {
		seg, rest, more := strings.Cut(s, ".")
		if !more {
			c.quoteTo(sb, c.prefix+seg) // only the last segment is the table name
			return
		}
		c.quoteTo(sb, seg)
		sb.WriteByte('.')
		s = rest
	}
}

// bareColumn strips a table qualifier and alias: "t.id", "id as x" -> "id" / "x".
func bareColumn(c string) string {
	if _, alias, ok := matchAlias(c); ok {
		return strings.TrimSpace(alias)
	}
	if i := strings.LastIndex(c, "."); i >= 0 {
		return c[i+1:]
	}
	return c
}
