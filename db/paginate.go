package db

import (
	"context"
	"fmt"
	"reflect"

	"github.com/akfaiz/quill/internal/rowmap"

	"github.com/akfaiz/quill/internal/keyset"
)

// Page describes one page of a length-aware pagination.
//
// Example:
//
//	var users []User
//	p, err := conn.Table("users").OrderBy("id").Paginate(ctx, 2, 2, &users)
//	// with 5 matching rows: p.Total = 5, p.PerPage = 2, p.CurrentPage = 2
//	// p.LastPage = 3, p.From = 3, p.To = 4
//	// with no rows: p.LastPage = 1, p.From = 0, p.To = 0 and users is empty
type Page struct {
	Total       int64
	PerPage     int
	CurrentPage int
	LastPage    int
	From        int // 1-based index of the first row on the page; 0 when empty
	To          int
}

// SimplePage is a pagination without a total count.
//
// Example:
//
//	var users []User
//	p, err := conn.Table("users").OrderBy("id").SimplePaginate(ctx, 1, 2, &users)
//	// p.PerPage = 2, p.CurrentPage = 1, p.HasMore = true when a third row exists
type SimplePage struct {
	PerPage     int
	CurrentPage int
	HasMore     bool
}

// CursorPage is a keyset pagination result.
//
// Example:
//
//	var users []User
//	p, err := conn.Table("users").CursorPaginate(ctx, 10, "", &users)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 11
//	// p.HasMore = false and p.NextCursor = "" when 10 or fewer rows remain
type CursorPage struct {
	PerPage    int
	HasMore    bool
	NextCursor string // pass to the next CursorPaginate call; empty when !HasMore
}

// CursorOptions selects the keyset column. Column must be unique and ordered
// (default "id"); ties would skip or repeat rows.
//
// Example:
//
//	opt := db.CursorOptions{Column: "id", Desc: true}
//	p, err := conn.Table("orders").CursorPaginate(ctx, 5, "", &orders, opt)
//	// SELECT * FROM "orders" ORDER BY "id" DESC LIMIT 6
type CursorOptions struct {
	Column string
	Desc   bool
}

func checkPaging(page, perPage int) (int, error) {
	if perPage < 1 {
		return 0, fmt.Errorf("%w: perPage must be at least 1", ErrInvalidArgs)
	}
	if page < 1 {
		page = 1
	}
	return page, nil
}

func setEmptySlice(dest any) {
	if rv := reflect.ValueOf(dest); rv.Kind() == reflect.Pointer && !rv.IsNil() && rv.Elem().Kind() == reflect.Slice {
		rv.Elem().Set(reflect.MakeSlice(rv.Elem().Type(), 0, 0))
	}
}

func sliceLen(dest any) int { return reflect.ValueOf(dest).Elem().Len() }

func truncateSlice(dest any, n int) {
	s := reflect.ValueOf(dest).Elem()
	s.Set(s.Slice(0, n))
}

// Paginate counts the rows, then scans the requested 1-based page into dest
// (a pointer to a slice). The count ignores order, limit and offset.
//
// Example:
//
//	var users []User
//	p, err := conn.Table("users").OrderBy("id").Paginate(ctx, 2, 2, &users)
//	// SELECT COUNT(*) AS "aggregate" FROM "users"
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 2 OFFSET 2
//	// with 5 rows: p.Total = 5, p.LastPage = 3, p.From = 3, p.To = 4
func (b *Builder) Paginate(ctx context.Context, page, perPage int, dest any) (Page, error) {
	page, err := checkPaging(page, perPage)
	if err != nil {
		return Page{}, err
	}
	counter := b.Clone()
	counter.orders, counter.unionOrders = nil, nil
	counter.limit, counter.offset, counter.unionLimit, counter.unionOffset = nil, nil, nil, nil
	total, err := counter.Count(ctx)
	if err != nil {
		return Page{}, err
	}
	p := Page{Total: total, PerPage: perPage, CurrentPage: page, LastPage: 1}
	if total > 0 {
		p.LastPage = int((total + int64(perPage) - 1) / int64(perPage))
	}
	if total == 0 {
		setEmptySlice(dest)
		return p, nil
	}
	if err := b.Clone().ForPage(page, perPage).Get(ctx, dest); err != nil {
		return Page{}, err
	}
	if n := sliceLen(dest); n > 0 {
		p.From = (page-1)*perPage + 1
		p.To = p.From + n - 1
	}
	return p, nil
}

// SimplePaginate fetches one extra row to learn whether another page exists,
// skipping the count query.
//
// Example:
//
//	var users []User
//	p, err := conn.Table("users").OrderBy("id").SimplePaginate(ctx, 1, 2, &users)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 3 OFFSET 0
//	// users holds at most 2 rows; p.HasMore = true when a third came back
func (b *Builder) SimplePaginate(ctx context.Context, page, perPage int, dest any) (SimplePage, error) {
	page, err := checkPaging(page, perPage)
	if err != nil {
		return SimplePage{}, err
	}
	if err := b.Clone().Offset((page-1)*perPage).Limit(perPage+1).Get(ctx, dest); err != nil {
		return SimplePage{}, err
	}
	p := SimplePage{PerPage: perPage, CurrentPage: page}
	if sliceLen(dest) > perPage {
		p.HasMore = true
		truncateSlice(dest, perPage)
	}
	return p, nil
}

// CursorPaginate pages forward by keyset on opt.Column. Pass the previous
// page's NextCursor (empty for the first page). It replaces any existing ORDER BY.
// The cursor is an opaque token; its value is only ever bound as an argument.
//
// Example:
//
//	var users []User
//	p, err := conn.Table("users").CursorPaginate(ctx, 2, "", &users)
//	// SELECT * FROM "users" ORDER BY "id" ASC LIMIT 3
//	// next page: conn.Table("users").CursorPaginate(ctx, 2, p.NextCursor, &users)
//	// SELECT * FROM "users" WHERE "id" > $1 ORDER BY "id" ASC LIMIT 3   args: [2]
func (b *Builder) CursorPaginate(
	ctx context.Context,
	perPage int,
	cursor string,
	dest any,
	opt ...CursorOptions,
) (CursorPage, error) {
	if _, err := checkPaging(1, perPage); err != nil {
		return CursorPage{}, err
	}
	o := CursorOptions{Column: "id"}
	if len(opt) > 0 {
		o = opt[0]
		if o.Column == "" {
			o.Column = "id"
		}
	}
	q := b.Clone().Reorder()
	dir, cmp := "asc", ">"
	if o.Desc {
		dir, cmp = "desc", "<"
	}
	if cursor != "" {
		v, err := keyset.Decode(cursor)
		if err != nil {
			return CursorPage{}, err
		}
		q.wheres = isolated(q.wheres)
		q.Where(o.Column, cmp, v)
	}
	if err := q.OrderBy(o.Column, dir).Limit(perPage+1).Get(ctx, dest); err != nil {
		return CursorPage{}, err
	}
	p := CursorPage{PerPage: perPage}
	if sliceLen(dest) <= perPage {
		return p, nil
	}
	truncateSlice(dest, perPage)
	last, err := rowmap.ValueOfColumn(reflect.ValueOf(dest).Elem().Index(perPage-1), bareColumn(o.Column))
	if err != nil {
		return CursorPage{}, err
	}
	p.HasMore = true
	p.NextCursor, err = keyset.Encode(last)
	return p, err
}
