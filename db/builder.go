package db

import (
	"fmt"
	"slices"
	"strings"

	"github.com/akfaiz/quill/internal/dialect"
	"github.com/akfaiz/quill/internal/jsonpath"
	"github.com/akfaiz/quill/internal/rawsql"
)

type clauseKind uint8

const (
	clauseBasic clauseKind = iota
	clauseNull
	clauseRaw
	clauseIn
	clauseInRaw
	clauseBetween
	clauseBetweenColumns
	clauseValueBetween
	clauseColumn
	clauseNested
	clauseExists
	clauseDate
	clauseRowValues
	clauseLike
	clauseNullSafe
	clauseJSONBool
	clauseJSONContains
	clauseJSONOverlaps
	clauseJSONContainsKey
	clauseJSONLength
)

// clause is one WHERE or HAVING predicate.
type clause struct {
	kind clauseKind
	conn connKind
	not  bool // is not null / not in / not between / not like
	cs   bool // case sensitive, for clauseLike

	column  string
	op      string
	value   any
	values  []any  // IN / BETWEEN / row-value lists
	column2 string // the right-hand column of clauseColumn
	x       *clauseExt
}

// clauseExt holds the fields only a few clause kinds use, so the common ones
// (basic, null, column, in) stay small: a query holds many clauses and copies
// them when slices grow and on Clone.
type clauseExt struct {
	columns []string
	nested  []clause
	sub     *Builder
	raw     Expression
	part    string // date part for clauseDate
}

// connKind is how a clause joins the one before it. The two "not" forms only
// differ from and/or on the first clause, where they write a leading "not".
type connKind uint8

const (
	connAnd connKind = iota
	connOr
	connAndNot
	connOrNot
)

func parseConn(s string) connKind {
	switch s {
	case "or":
		return connOr
	case "and not":
		return connAndNot
	case "or not":
		return connOrNot
	}
	return connAnd
}

func (k connKind) String() string {
	switch k {
	case connOr:
		return "OR"
	case connAndNot:
		return "AND NOT"
	case connOrNot:
		return "OR NOT"
	}
	return "AND"
}

func (k connKind) negated() bool { return k == connAndNot || k == connOrNot }

// Nil-safe views of the extension fields.
func (c *clause) subquery() *Builder {
	if c.x == nil {
		return nil
	}
	return c.x.sub
}

func (c *clause) cols() []string {
	if c.x == nil {
		return nil
	}
	return c.x.columns
}

func (c *clause) kids() []clause {
	if c.x == nil {
		return nil
	}
	return c.x.nested
}

func (c *clause) rawExpr() Expression {
	if c.x == nil {
		return Expression{}
	}
	return c.x.raw
}

func (c *clause) datePart() string {
	if c.x == nil {
		return ""
	}
	return c.x.part
}

type selectItem struct {
	column string
	raw    *Expression
	sub    *Builder // select-list subquery; alias is its column alias
	alias  string
}

type unionClause struct {
	q   *Builder
	all bool
}

type indexHint struct {
	kind  string // use, force, ignore
	index string
}

type orderItem struct {
	column string
	dir    string
	raw    *Expression
}

// Builder is a mutable, fluent SQL query builder. Not safe for concurrent use;
// call Clone to branch a query.
type Builder struct {
	db      *DB // set by DB.Table; nil for build-only builders
	dialect Dialect
	prefix  string
	err     error

	table     string
	fromSub   *Builder
	fromAlias string
	fromRaw   *Expression
	columns   []selectItem
	joins     []joinClause
	distinct  bool
	wheres    []clause
	groups    []selectItem
	havings   []clause
	orders    []orderItem
	limit     *int
	offset    *int

	ctes []cteClause

	unions      []unionClause
	unionOrders []orderItem
	unionLimit  *int
	unionOffset *int

	returning []string
	allRows   bool

	lock    dialect.LockKind
	lockRaw string
	hint    *indexHint
}

// NewBuilder returns a builder that compiles for the given dialect.
func NewBuilder(d Dialect) *Builder {
	b := &Builder{dialect: d}
	if _, err := d.grammar(); err != nil {
		b.err = err
	}
	return b
}

func (b *Builder) fail(err error) *Builder {
	if b.err == nil {
		b.err = err
	}
	return b
}

// Err returns the first error recorded while building.
func (b *Builder) Err() error { return b.err }

// Prefix sets a table prefix applied to table names, aliases and qualified columns.
func (b *Builder) Prefix(p string) *Builder { b.prefix = p; return b }

// Clone returns an independent copy.
func (b *Builder) Clone() *Builder {
	n := *b
	n.columns = append([]selectItem(nil), b.columns...)
	n.joins = cloneJoins(b.joins)
	n.wheres = cloneClauses(b.wheres)
	n.groups = append([]selectItem(nil), b.groups...)
	n.havings = cloneClauses(b.havings)
	n.orders = append([]orderItem(nil), b.orders...)
	n.unionOrders = append([]orderItem(nil), b.unionOrders...)
	for i := range n.columns {
		if n.columns[i].sub != nil {
			n.columns[i].sub = n.columns[i].sub.Clone()
		}
	}
	if b.fromSub != nil {
		n.fromSub = b.fromSub.Clone()
	}
	n.ctes = cloneCTEs(b.ctes)
	n.unions = make([]unionClause, len(b.unions))
	for i, u := range b.unions {
		n.unions[i] = unionClause{q: u.q.Clone(), all: u.all}
	}
	if b.hint != nil {
		h := *b.hint
		n.hint = &h
	}
	n.returning = append([]string(nil), b.returning...)
	n.unionLimit = cloneInt(b.unionLimit)
	n.unionOffset = cloneInt(b.unionOffset)
	n.limit = cloneInt(b.limit)
	n.offset = cloneInt(b.offset)
	return &n
}

func cloneInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// cloneClauses copies clauses, deep-cloning nested groups and subqueries.
func cloneClauses(cs []clause) []clause {
	if cs == nil {
		return nil
	}
	out := make([]clause, len(cs))
	copy(out, cs)
	for i := range out {
		if x := out[i].x; x != nil {
			n := *x
			n.nested = cloneClauses(n.nested)
			if n.sub != nil {
				n.sub = n.sub.Clone()
			}
			out[i].x = &n
		}
	}
	return out
}

// From sets the table ("users", "users as u", "schema.users").
func (b *Builder) From(table string) *Builder {
	b.table, b.fromSub, b.fromRaw = table, nil, nil
	return b
}

// Select replaces the selected columns.
func (b *Builder) Select(columns ...string) *Builder {
	b.columns = nil
	return b.AddSelect(columns...)
}

// AddSelect appends columns.
func (b *Builder) AddSelect(columns ...string) *Builder {
	b.columns = slices.Grow(b.columns, len(columns))
	for _, c := range columns {
		b.columns = append(b.columns, selectItem{column: c})
	}
	return b
}

// SelectRaw appends a raw select expression.
func (b *Builder) SelectRaw(sql string, args ...any) *Builder {
	e := Raw(sql, args...)
	b.columns = append(b.columns, selectItem{raw: &e})
	return b
}

// Distinct marks the query SELECT DISTINCT.
func (b *Builder) Distinct() *Builder { b.distinct = true; return b }

// Where adds an AND predicate. Forms: Where(col, value) means "=";
// Where(col, op, value). A nil value with "=" / "!=" / "<>" becomes IS [NOT] NULL.
func (b *Builder) Where(column string, args ...any) *Builder {
	return b.addClause(&b.wheres, "and", column, args)
}

// OrWhere is Where joined with OR.
func (b *Builder) OrWhere(column string, args ...any) *Builder {
	return b.addClause(&b.wheres, "or", column, args)
}

// WhereRaw adds a raw predicate (caller-trusted SQL).
func (b *Builder) WhereRaw(sql string, args ...any) *Builder {
	b.wheres = append(b.wheres, clause{kind: clauseRaw, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// OrWhereRaw is WhereRaw joined with OR.
func (b *Builder) OrWhereRaw(sql string, args ...any) *Builder {
	b.wheres = append(b.wheres, clause{kind: clauseRaw, conn: connOr, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// GroupBy adds GROUP BY columns.
func (b *Builder) GroupBy(columns ...string) *Builder {
	b.groups = slices.Grow(b.groups, len(columns))
	for _, c := range columns {
		b.groups = append(b.groups, selectItem{column: c})
	}
	return b
}

// GroupByRaw adds a raw GROUP BY expression.
func (b *Builder) GroupByRaw(sql string, args ...any) *Builder {
	e := Raw(sql, args...)
	b.groups = append(b.groups, selectItem{raw: &e})
	return b
}

// Having adds an AND HAVING predicate; same argument forms as Where.
func (b *Builder) Having(column string, args ...any) *Builder {
	return b.addClause(&b.havings, "and", column, args)
}

// OrHaving is Having joined with OR.
func (b *Builder) OrHaving(column string, args ...any) *Builder {
	return b.addClause(&b.havings, "or", column, args)
}

// HavingRaw adds a raw HAVING predicate (caller-trusted SQL).
func (b *Builder) HavingRaw(sql string, args ...any) *Builder {
	b.havings = append(b.havings, clause{kind: clauseRaw, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// OrHavingRaw is HavingRaw joined with OR.
func (b *Builder) OrHavingRaw(sql string, args ...any) *Builder {
	b.havings = append(b.havings, clause{kind: clauseRaw, conn: connOr, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// OrderBy adds ORDER BY column; direction is "asc" (default) or "desc".
func (b *Builder) OrderBy(column string, direction ...string) *Builder {
	dir := "ASC"
	if len(direction) > 0 {
		dir = strings.ToUpper(direction[0])
	}
	if dir != "ASC" && dir != "DESC" {
		return b.fail(ErrInvalidDirection)
	}
	b.addOrder(orderItem{column: column, dir: dir})
	return b
}

// OrderByDesc is OrderBy(column, "desc").
func (b *Builder) OrderByDesc(column string) *Builder { return b.OrderBy(column, "desc") }

// OrderByRaw adds a raw ORDER BY expression (caller-trusted SQL).
func (b *Builder) OrderByRaw(sql string, args ...any) *Builder {
	e := Raw(sql, args...)
	b.addOrder(orderItem{raw: &e})
	return b
}

// Limit sets LIMIT; negative values are ignored (as in Laravel).
func (b *Builder) Limit(n int) *Builder {
	if n >= 0 {
		if len(b.unions) > 0 {
			b.unionLimit = &n
		} else {
			b.limit = &n
		}
	}
	return b
}

// addOrder appends to the union's ORDER BY once a union exists (as Laravel does).
func (b *Builder) addOrder(o orderItem) {
	if len(b.unions) > 0 {
		b.unionOrders = append(b.unionOrders, o)
		return
	}
	b.orders = append(b.orders, o)
}

// Take is an alias of Limit.
func (b *Builder) Take(n int) *Builder { return b.Limit(n) }

// Offset sets OFFSET; negative values clamp to 0.
func (b *Builder) Offset(n int) *Builder {
	if n < 0 {
		n = 0
	}
	if len(b.unions) > 0 {
		b.unionOffset = &n
	} else {
		b.offset = &n
	}
	return b
}

// Skip is an alias of Offset.
func (b *Builder) Skip(n int) *Builder { return b.Offset(n) }

// ForPage sets offset/limit for a 1-based page.
func (b *Builder) ForPage(page, perPage int) *Builder {
	if page < 1 {
		page = 1
	}
	return b.Offset((page - 1) * perPage).Limit(perPage)
}

// When applies fn when cond is true.
func (b *Builder) When(cond bool, fn func(*Builder)) *Builder {
	if cond {
		fn(b)
	}
	return b
}

// Unless applies fn when cond is false.
func (b *Builder) Unless(cond bool, fn func(*Builder)) *Builder { return b.When(!cond, fn) }

// Tap runs fn for side effects and returns the builder.
func (b *Builder) Tap(fn func(*Builder)) *Builder { fn(b); return b }

// operators maps each allowed operator, lowercased, to the SQL written for it.
var operators = map[string]string{
	"=": "=", "<": "<", ">": ">", "<=": "<=", ">=": ">=", "<>": "<>", "!=": "!=", "<=>": "<=>",
	"like": "LIKE", "not like": "NOT LIKE", "ilike": "ILIKE", "not ilike": "NOT ILIKE", "like binary": "LIKE BINARY",
	"&": "&", "|": "|", "^": "^", "<<": "<<", ">>": ">>", "&~": "&~",
	"is": "IS", "is not": "IS NOT",
	"rlike": "RLIKE", "not rlike": "NOT RLIKE", "regexp": "REGEXP", "not regexp": "NOT REGEXP",
	"~": "~", "~*": "~*", "!~": "!~", "!~*": "!~*", "similar to": "SIMILAR TO", "not similar to": "NOT SIMILAR TO",
	"~~*": "~~*", "!~~*": "!~~*",
}

// parseOpValue resolves the (value) / (operator, value) argument forms.
func parseOpValue(column string, args []any) (op string, val any, err error) {
	switch len(args) {
	case 1:
		return "=", args[0], nil
	case 2:
		s, ok := args[0].(string)
		if !ok {
			return "", nil, fmt.Errorf("%w: operator must be a string", ErrInvalidOperator)
		}
		op = strings.ToLower(strings.TrimSpace(s))
		canon, ok := operators[op]
		if !ok {
			return "", nil, fmt.Errorf("%w: %q", ErrInvalidOperator, s)
		}
		val = args[1]
		if val == nil {
			switch op {
			case "=", "is", "!=", "<>", "is not":
			default:
				return "", nil, fmt.Errorf("%w: operator %q needs a non-nil value", ErrInvalidArgs, op)
			}
		}
		return canon, val, nil
	}
	return "", nil, fmt.Errorf("%w: %q takes (value) or (operator, value)", ErrInvalidArgs, column)
}

func (b *Builder) addClause(dst *[]clause, boolean, column string, args []any) *Builder {
	op, val, err := parseOpValue(column, args)
	if err != nil {
		return b.fail(err)
	}
	if val == nil {
		return b.push(
			dst,
			boolean,
			clause{kind: clauseNull, column: column, not: op == "!=" || op == "<>" || op == "IS NOT"},
		)
	}
	if _, isBool := val.(bool); isBool && jsonpath.IsSelector(column) {
		return b.push(dst, boolean, clause{kind: clauseJSONBool, column: column, op: op, value: val})
	}
	return b.push(dst, boolean, clause{kind: clauseBasic, column: column, op: op, value: val})
}

// NewQuery returns an empty builder sharing this builder's dialect and prefix.
func (b *Builder) NewQuery() *Builder {
	return &Builder{dialect: b.dialect, prefix: b.prefix, err: b.err}
}

// ToSQL compiles a SELECT into SQL text and its ordered bind arguments.
func (b *Builder) ToSQL() (string, []any, error) {
	c, err := b.newCompiler()
	if err != nil {
		return "", nil, err
	}
	c.reserve(b)
	if err := c.selectQuery(b); err != nil {
		return "", nil, err
	}
	if len(c.args) == 0 {
		c.args = nil // the reserved capacity is not an observable empty slice
	}
	return c.sb.String(), c.args, nil
}

// newCompiler returns a compiler for this builder's dialect, or the first build error.
func (b *Builder) newCompiler() (*compiler, error) {
	if b.err != nil {
		return nil, b.err
	}
	g, err := b.dialect.grammar()
	if err != nil {
		return nil, err
	}
	return &compiler{d: b.dialect, g: g, prefix: b.prefix}, nil
}

// MustSQL is ToSQL that panics on error instead of returning it. Use it for
// fixed queries (tests, examples, package-level vars) where an error means a
// programming mistake. Do not use it where an error can come from input, such as a
// user-chosen operator or column: handle ToSQL's error there. The panic value is
// the error, wrapped, so errors.Is still works on a recovered value.
func (b *Builder) MustSQL() (string, []any) {
	sql, args, err := b.ToSQL()
	if err != nil {
		panic(fmt.Errorf("quill: MustSQL: %w", err))
	}
	return sql, args
}

// MustRawSQL is ToRawSQL that panics on error. DEBUG ONLY, like ToRawSQL: the
// result is not injection-safe. See MustSQL for when panicking is appropriate.
func (b *Builder) MustRawSQL() string {
	sql, err := b.ToRawSQL()
	if err != nil {
		panic(fmt.Errorf("quill: MustRawSQL: %w", err))
	}
	return sql
}

// ToRawSQL compiles and interpolates args into the SQL.
// DEBUG ONLY: the result is not injection-safe. Never execute or log it in production.
func (b *Builder) ToRawSQL() (string, error) {
	sql, args, err := b.ToSQL()
	if err != nil {
		return "", err
	}
	return rawsql.Interpolate(sql, args, b.dialect == Postgres), nil
}
