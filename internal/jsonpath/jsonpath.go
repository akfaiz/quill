// Package jsonpath renders the "column->a->b[0]" selector syntax into the JSON
// path literals and operands each database expects. The functions are pure
// string builders; quoting here is what keeps a path from breaking out of its
// SQL literal.
package jsonpath

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/akfaiz/quill/internal/errs"
)

var (
	quoteRe    = regexp.MustCompile(`(\\+)?'`)
	arrayEndRe = regexp.MustCompile(`(\[[^\]]+\])+$`)
	arrayKeyRe = regexp.MustCompile(`\[([^\]]+)\]`)
	indexEndRe = regexp.MustCompile(`\[(-?[0-9]+)\]$`)
	intRe      = regexp.MustCompile(`^-?[0-9]+$`)
)

// IsSelector reports whether s uses the "->" JSON path syntax.
func IsSelector(s string) bool { return strings.Contains(s, "->") }

// SplitKey splits "tbl.col->a->b" into the column part and its path segments.
func SplitKey(key string) (column string, path []string) {
	parts := strings.Split(key, "->")
	return parts[0], parts[1:]
}

// LastSegment returns what follows the last "." ("tbl.col" -> "col").
func LastSegment(s string) string {
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Path renders a MySQL/SQLite JSON path literal for "a->b[0]": '$."a"."b"[0]'.
// Single quotes are doubled (and backslashes before them dropped) so the
// literal cannot be escaped.
func Path(v string) string {
	v = quoteRe.ReplaceAllString(v, "''")
	segs := strings.Split(v, "->")
	for i, s := range segs {
		segs[i] = segment(s)
	}
	p := strings.Join(segs, ".")
	if strings.HasPrefix(p, "[") {
		return "'$" + p + "'"
	}
	return "'$." + p + "'"
}

func segment(seg string) string {
	if loc := arrayEndRe.FindStringIndex(seg); loc != nil {
		if key := seg[:loc[0]]; key != "" {
			return `"` + key + `"` + seg[loc[0]:]
		}
		return seg[loc[0]:]
	}
	return `"` + seg + `"`
}

// PgAttributes turns path segments into Postgres path operands, expanding
// "key[0][1]" and quoting non-integers with quote ("'" for -> operands, `"`
// inside a '{...}' path).
func PgAttributes(path []string, quote string) []string {
	var out []string
	for _, p := range path {
		var attrs []string
		if loc := arrayEndRe.FindStringIndex(p); loc != nil {
			attrs = append(attrs, p[:loc[0]])
			for _, m := range arrayKeyRe.FindAllStringSubmatch(p[loc[0]:], -1) {
				attrs = append(attrs, m[1])
			}
		} else {
			attrs = []string{p}
		}
		for _, a := range attrs {
			if a == "" {
				continue
			}
			if intRe.MatchString(a) {
				out = append(out, a)
				continue
			}
			a = strings.ReplaceAll(a, "'", "''")
			if quote != "'" {
				a = strings.ReplaceAll(a, quote, quote+quote)
			}
			out = append(out, quote+a+quote)
		}
	}
	return out
}

// ParseIndexSegment recognises a path's last segment as an array index, either
// a bare integer ("2") or a keyed one ("tags[-2]"). key is what precedes the
// index ("" for a bare integer).
func ParseIndexSegment(last string) (key string, index int, ok bool) {
	if intRe.MatchString(last) {
		n, _ := strconv.Atoi(last)
		return "", n, true
	}
	if m := indexEndRe.FindStringSubmatchIndex(last); m != nil {
		n, _ := strconv.Atoi(last[m[2]:m[3]])
		return last[:m[0]], n, true
	}
	return "", 0, false
}

// Encode marshals v as JSON without HTML escaping (<, > and & stay literal).
func Encode(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", fmt.Errorf("%w: cannot encode JSON: %v", errs.ErrInvalidArgs, err)
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// IsContainer reports values that encode as a JSON object or array (maps,
// structs, slices other than []byte), as opposed to a scalar.
func IsContainer(v any) bool {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Struct:
		return true
	case reflect.Slice, reflect.Array:
		return rv.Type().Elem().Kind() != reflect.Uint8
	}
	return false
}
