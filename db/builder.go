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
	sub     *query
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
func (c *clause) subquery() *query {
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
	sub    *query // select-list subquery; alias is its column alias
	alias  string
}

type unionClause struct {
	q   *query
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

// query holds the clause state shared by the statement builders (SelectBuilder,
// UpdateBuilder, DeleteBuilder). Not safe for concurrent use.
type query struct {
	db      *DB // set by DB.Select and friends; nil for build-only builders
	dialect Dialect
	prefix  string
	err     error

	table     string
	fromSub   *query
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

func (b *query) fail(err error) *query {
	if b.err == nil {
		b.err = err
	}
	return b
}

// Err returns the first error recorded while building.
//
// Example:
//
//	q := conn.Select().From("users").Where("id", "!!", 1)
//	err := q.Err()
//	// err: quill: invalid operator: "!!"
func (b *query) Err() error { return b.err }

// Prefix sets a table prefix applied to table names, aliases and qualified columns.
//
// Example:
//
//	db.Use(db.Postgres).Select("u.id").From("users as u").Prefix("app_")
//	// SELECT "app_u"."id" FROM "app_users" AS "app_u"
func (b *query) Prefix(p string) *query { b.prefix = p; return b }

// Clone returns an independent copy.
//
// Example:
//
//	base := conn.Select().From("users").Where("active", true)
//	admins := base.Clone().Where("role", "admin")
//	// base:   SELECT * FROM "users" WHERE "active" = $1
//	// admins: SELECT * FROM "users" WHERE "active" = $1 AND "role" = $2
//	// args: [true admin] (admins)
//
// Cloning a nil builder returns nil.
func (b *query) Clone() *query {
	if b == nil {
		return nil
	}
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
//
// Example:
//
//	db.Use(db.Postgres).Select().From("public.users as u")
//	// SELECT * FROM "public"."users" AS "u"
func (b *query) From(table string) *query {
	b.table, b.fromSub, b.fromRaw = table, nil, nil
	return b
}

// Select replaces the selected columns.
//
// Example:
//
//	conn.Select().From("users as u").Select("u.id", "u.name as full_name")
//	// SELECT "u"."id", "u"."name" AS "full_name" FROM "users" AS "u"
func (b *query) Select(columns ...string) *query {
	b.columns = nil
	return b.AddSelect(columns...)
}

// AddSelect appends columns.
//
// Example:
//
//	conn.Select().From("users").Select("id").AddSelect("name", "age")
//	// SELECT "id", "name", "age" FROM "users"
func (b *query) AddSelect(columns ...string) *query {
	b.columns = slices.Grow(b.columns, len(columns))
	for _, c := range columns {
		b.columns = append(b.columns, selectItem{column: c})
	}
	return b
}

// SelectRaw appends a raw select expression.
//
// Example:
//
//	conn.Select().From("orders").Select("user_id").
//		SelectRaw("sum(amount) * ? as total", 1.1).GroupBy("user_id")
//	// SELECT "user_id", sum(amount) * $1 as total FROM "orders" GROUP BY "user_id"
//	// args: [1.1]
func (b *query) SelectRaw(sql string, args ...any) *query {
	e := Raw(sql, args...)
	b.columns = append(b.columns, selectItem{raw: &e})
	return b
}

// Distinct marks the query SELECT DISTINCT.
//
// Example:
//
//	conn.Select().From("posts").Distinct().Select("user_id")
//	// SELECT DISTINCT "user_id" FROM "posts"
func (b *query) Distinct() *query { b.distinct = true; return b }

// Where adds an AND predicate. Forms: Where(col, value) means "=";
// Where(col, op, value). A nil value with "=" / "!=" / "<>" becomes IS [NOT] NULL.
//
// Example:
//
//	conn.Select().From("users").Where("active", true).Where("age", ">=", 18)
//	// SELECT * FROM "users" WHERE "active" = $1 AND "age" >= $2
//	// args: [true 18]
func (b *query) Where(column string, args ...any) *query {
	return b.addClause(&b.wheres, "and", column, args)
}

// OrWhere is Where joined with OR.
//
// Example:
//
//	conn.Select().From("users").Where("role", "admin").OrWhere("role", "owner")
//	// SELECT * FROM "users" WHERE "role" = $1 OR "role" = $2
//	// args: [admin owner]
func (b *query) OrWhere(column string, args ...any) *query {
	return b.addClause(&b.wheres, "or", column, args)
}

// WhereRaw adds a raw predicate (caller-trusted SQL).
//
// Example:
//
//	conn.Select().From("users").WhereRaw("lower(name) = ?", "ann")
//	// SELECT * FROM "users" WHERE lower(name) = $1
//	// args: [ann]
func (b *query) WhereRaw(sql string, args ...any) *query {
	b.wheres = append(b.wheres, clause{kind: clauseRaw, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// OrWhereRaw is WhereRaw joined with OR.
//
// Example:
//
//	conn.Select().From("users").Where("active", true).OrWhereRaw("age between ? and ?", 18, 30)
//	// SELECT * FROM "users" WHERE "active" = $1 OR age between $2 and $3
//	// args: [true 18 30]
func (b *query) OrWhereRaw(sql string, args ...any) *query {
	b.wheres = append(b.wheres, clause{kind: clauseRaw, conn: connOr, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// GroupBy adds GROUP BY columns.
//
// Example:
//
//	conn.Select().From("orders").Select("user_id", "status").GroupBy("user_id", "status")
//	// SELECT "user_id", "status" FROM "orders" GROUP BY "user_id", "status"
func (b *query) GroupBy(columns ...string) *query {
	b.groups = slices.Grow(b.groups, len(columns))
	for _, c := range columns {
		b.groups = append(b.groups, selectItem{column: c})
	}
	return b
}

// GroupByRaw adds a raw GROUP BY expression.
//
// Example:
//
//	conn.Select().From("orders").SelectRaw("date_trunc('day', created_at) as day").
//		GroupByRaw("date_trunc('day', created_at)")
//	// SELECT date_trunc('day', created_at) as day FROM "orders" GROUP BY date_trunc('day',
//	//     created_at)
func (b *query) GroupByRaw(sql string, args ...any) *query {
	e := Raw(sql, args...)
	b.groups = append(b.groups, selectItem{raw: &e})
	return b
}

// Having adds an AND HAVING predicate; same argument forms as Where.
//
// Example:
//
//	conn.Select().From("orders").Select("user_id").GroupBy("user_id").
//		Having("user_id", ">", 10)
//	// SELECT "user_id" FROM "orders" GROUP BY "user_id" HAVING "user_id" > $1
//	// args: [10]
func (b *query) Having(column string, args ...any) *query {
	return b.addClause(&b.havings, "and", column, args)
}

// OrHaving is Having joined with OR.
//
// Example:
//
//	conn.Select().From("orders").Select("user_id").GroupBy("user_id").
//		Having("user_id", ">", 10).OrHaving("user_id", 1)
//	// SELECT "user_id" FROM "orders" GROUP BY "user_id" HAVING "user_id" > $1 OR "user_id" = $2
//	// args: [10 1]
func (b *query) OrHaving(column string, args ...any) *query {
	return b.addClause(&b.havings, "or", column, args)
}

// HavingRaw adds a raw HAVING predicate (caller-trusted SQL).
//
// Example:
//
//	conn.Select().From("orders").Select("user_id").GroupBy("user_id").
//		HavingRaw("sum(amount) > ?", 100)
//	// SELECT "user_id" FROM "orders" GROUP BY "user_id" HAVING sum(amount) > $1
//	// args: [100]
func (b *query) HavingRaw(sql string, args ...any) *query {
	b.havings = append(b.havings, clause{kind: clauseRaw, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// OrHavingRaw is HavingRaw joined with OR.
//
// Example:
//
//	conn.Select().From("orders").Select("user_id").GroupBy("user_id").
//		HavingRaw("sum(amount) > ?", 100).OrHavingRaw("count(*) > ?", 5)
//	// SELECT "user_id" FROM "orders" GROUP BY "user_id" HAVING sum(amount) > $1 OR count(*) > $2
//	// args: [100 5]
func (b *query) OrHavingRaw(sql string, args ...any) *query {
	b.havings = append(b.havings, clause{kind: clauseRaw, conn: connOr, x: &clauseExt{raw: Raw(sql, args...)}})
	return b
}

// OrderBy adds ORDER BY column; direction is "asc" (default) or "desc".
//
// Example:
//
//	conn.Select().From("users").OrderBy("name").OrderBy("age", "desc")
//	// SELECT * FROM "users" ORDER BY "name" ASC, "age" DESC
func (b *query) OrderBy(column string, direction ...string) *query {
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
//
// Example:
//
//	conn.Select().From("users").OrderByDesc("created_at")
//	// SELECT * FROM "users" ORDER BY "created_at" DESC
func (b *query) OrderByDesc(column string) *query { return b.OrderBy(column, "desc") }

// OrderByRaw adds a raw ORDER BY expression (caller-trusted SQL).
//
// Example:
//
//	conn.Select().From("users").OrderByRaw("case when role = ? then 0 else 1 end", "admin")
//	// SELECT * FROM "users" ORDER BY case when role = $1 then 0 else 1 end
//	// args: [admin]
func (b *query) OrderByRaw(sql string, args ...any) *query {
	e := Raw(sql, args...)
	b.addOrder(orderItem{raw: &e})
	return b
}

// Limit sets LIMIT; negative values are ignored.
//
// Example:
//
//	conn.Select().From("users").Limit(10)
//	// SELECT * FROM "users" LIMIT 10
func (b *query) Limit(n int) *query {
	if n >= 0 {
		if len(b.unions) > 0 {
			b.unionLimit = &n
		} else {
			b.limit = &n
		}
	}
	return b
}

// addOrder appends to the union's ORDER BY once a union exists.
func (b *query) addOrder(o orderItem) {
	if len(b.unions) > 0 {
		b.unionOrders = append(b.unionOrders, o)
		return
	}
	b.orders = append(b.orders, o)
}

// Take is an alias of Limit.
//
// Example:
//
//	conn.Select().From("users").Take(5)
//	// SELECT * FROM "users" LIMIT 5
func (b *query) Take(n int) *query { return b.Limit(n) }

// Offset sets OFFSET; negative values clamp to 0.
//
// Example:
//
//	conn.Select().From("users").Limit(10).Offset(20)
//	// SELECT * FROM "users" LIMIT 10 OFFSET 20
func (b *query) Offset(n int) *query {
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
//
// Example:
//
//	conn.Select().From("users").Take(10).Skip(5)
//	// SELECT * FROM "users" LIMIT 10 OFFSET 5
func (b *query) Skip(n int) *query { return b.Offset(n) }

// ForPage sets offset/limit for a 1-based page.
//
// Example:
//
//	conn.Select().From("users").OrderBy("id").ForPage(3, 25) // page 3, 25 rows per page
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 25 OFFSET 50
func (b *query) ForPage(page, perPage int) *query {
	if page < 1 {
		page = 1
	}
	return b.Offset((page - 1) * perPage).Limit(perPage)
}

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

func (b *query) addClause(dst *[]clause, boolean, column string, args []any) *query {
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

// fresh returns an empty query sharing this one's dialect, prefix and error.
func (b *query) fresh() *query {
	return &query{dialect: b.dialect, prefix: b.prefix, err: b.err}
}

// ToSQL compiles a SELECT into SQL text and its ordered bind arguments.
//
// Example:
//
//	sql, args, err := conn.Select().From("users").Where("age", ">", 18).Limit(10).ToSQL()
//	// SELECT * FROM "users" WHERE "age" > $1 LIMIT 10
//	// args: [18]
func (b *query) ToSQL() (string, []any, error) {
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
func (b *query) newCompiler() (*compiler, error) {
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
//
// Example:
//
//	sql, args := conn.Select().From("users").Where("id", 42).MustSQL()
//	// SELECT * FROM "users" WHERE "id" = $1
//	// args: [42]
func (b *query) MustSQL() (string, []any) {
	sql, args, err := b.ToSQL()
	if err != nil {
		panic(fmt.Errorf("quill: MustSQL: %w", err))
	}
	return sql, args
}

// MustRawSQL is ToRawSQL that panics on error. DEBUG ONLY, like ToRawSQL: the
// result is not injection-safe. See MustSQL for when panicking is appropriate.
//
// Example:
//
//	sql := conn.Select().From("users").Where("id", 42).Where("name", "ann").MustRawSQL()
//	// sql: SELECT * FROM "users" WHERE "id" = 42 AND "name" = 'ann'
//	// DEBUG ONLY: not injection-safe
func (b *query) MustRawSQL() string {
	sql, err := b.ToRawSQL()
	if err != nil {
		panic(fmt.Errorf("quill: MustRawSQL: %w", err))
	}
	return sql
}

// ToRawSQL compiles and interpolates args into the SQL.
// DEBUG ONLY: the result is not injection-safe. Never execute or log it in production.
//
// Example:
//
//	sql, err := conn.Select().From("users").Where("id", 42).Where("name", "ann").ToRawSQL()
//	// sql: SELECT * FROM "users" WHERE "id" = 42 AND "name" = 'ann'
//	// DEBUG ONLY: not injection-safe
func (b *query) ToRawSQL() (string, error) {
	sql, args, err := b.ToSQL()
	if err != nil {
		return "", err
	}
	return rawsql.Interpolate(sql, args, b.dialect == Postgres), nil
}
