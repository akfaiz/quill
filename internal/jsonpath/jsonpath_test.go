package jsonpath

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/akfaiz/quill/internal/errs"
)

func TestPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"theme", `'$."theme"'`},
		{"a->b", `'$."a"."b"'`},
		{"x[0]->y", `'$."x"[0]."y"'`},
		{"[2]", `'$[2]'`},
		{"tags[0][1]", `'$."tags"[0][1]'`},
		{`a'b`, `'$."a''b"'`},
		{`a\'b`, `'$."a''b"'`}, // backslashes before a quote are dropped
		{`a\\'b`, `'$."a''b"'`},
	}
	for _, tc := range tests {
		if got := Path(tc.in); got != tc.want {
			t.Errorf("Path(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestPgAttributes(t *testing.T) {
	tests := []struct {
		name  string
		path  []string
		quote string
		want  []string
	}{
		{"plain", []string{"a", "b"}, "'", []string{"'a'", "'b'"}},
		{"integers stay bare", []string{"a", "0", "-1"}, "'", []string{"'a'", "0", "-1"}},
		{"array keys expand", []string{"x[0][1]", "y"}, "'", []string{"'x'", "0", "1", "'y'"}},
		{"leading index drops the empty key", []string{"[3]"}, "'", []string{"3"}},
		{"single quote doubled", []string{"a'b"}, "'", []string{"'a''b'"}},
		{"double quote path", []string{"a", `b"c`}, `"`, []string{`"a"`, `"b""c"`}},
		{"both escapes in a double-quoted path", []string{`it's "x"`}, `"`, []string{`"it''s ""x"""`}},
		{"empty", nil, "'", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PgAttributes(tc.path, tc.quote); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseIndexSegment(t *testing.T) {
	tests := []struct {
		in    string
		key   string
		index int
		ok    bool
	}{
		{"2", "", 2, true},
		{"-3", "", -3, true},
		{"tags[1]", "tags", 1, true},
		{"tags[-2]", "tags", -2, true},
		{"a[1][2]", "a[1]", 2, true},
		{"[4]", "", 4, true},
		{"tags", "", 0, false},
		{"tags[x]", "", 0, false},
		{"", "", 0, false},
	}
	for _, tc := range tests {
		key, index, ok := ParseIndexSegment(tc.in)
		if key != tc.key || index != tc.index || ok != tc.ok {
			t.Errorf("ParseIndexSegment(%q) = (%q, %d, %v), want (%q, %d, %v)", tc.in, key, index, ok, tc.key, tc.index, tc.ok)
		}
	}
}

func TestSelectorHelpers(t *testing.T) {
	if !IsSelector("a->b") || IsSelector("a.b") || IsSelector("") {
		t.Error("IsSelector")
	}
	col, path := SplitKey("t.opts->a->b")
	if col != "t.opts" || !reflect.DeepEqual(path, []string{"a", "b"}) {
		t.Errorf("SplitKey: %q %q", col, path)
	}
	if col, path = SplitKey("plain"); col != "plain" || len(path) != 0 {
		t.Errorf("SplitKey without a path: %q %q", col, path)
	}
	for in, want := range map[string]string{"t.c": "c", "c": "c", "a.b.c": "c", "": ""} {
		if got := LastSegment(in); got != want {
			t.Errorf("LastSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEncode(t *testing.T) {
	got, err := Encode(map[string]any{"q": "a<b&c>", "n": 1})
	if err != nil || got != `{"n":1,"q":"a<b&c>"}` {
		t.Errorf("got %q, %v", got, err)
	}
	if got, _ := Encode("x"); got != `"x"` {
		t.Errorf("scalar: %q", got)
	}
	if _, err := Encode(make(chan int)); !errors.Is(err, errs.ErrInvalidArgs) {
		t.Errorf("unencodable value: %v", err)
	}
}

func TestIsContainer(t *testing.T) {
	type s struct{ A int }
	for v, want := range map[string]bool{"map": true, "struct": true, "slice": true, "array": true,
		"bytes": false, "string": false, "int": false, "nil": false} {
		var x any
		switch v {
		case "map":
			x = map[string]int{}
		case "struct":
			x = s{}
		case "slice":
			x = []string{"a"}
		case "array":
			x = [2]int{}
		case "bytes":
			x = []byte("x")
		case "string":
			x = "x"
		case "int":
			x = 1
		}
		if got := IsContainer(x); got != want {
			t.Errorf("IsContainer(%s) = %v, want %v", v, got, want)
		}
	}
}

// FuzzPathCannotEscapeItsLiteral: whatever the path, every single quote inside
// the rendered literal must be doubled, so it cannot terminate the SQL string.
func FuzzPathCannotEscapeItsLiteral(f *testing.F) {
	for _, s := range []string{"a", "a->b", `a'b`, `a\'b`, `a\\\'b`, "x[0]", "'", "''", `\`, "->", "a->'--"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := Path(in)
		if !strings.HasPrefix(out, "'$") || !strings.HasSuffix(out, "'") {
			t.Fatalf("Path(%q) = %s: not a quoted $ path", in, out)
		}
		inner := out[1 : len(out)-1]
		if strings.Count(strings.ReplaceAll(inner, "''", ""), "'") != 0 {
			t.Fatalf("Path(%q) = %s: contains an unescaped quote", in, out)
		}
	})
}

// FuzzPgAttributesQuoting: every non-integer operand is wrapped in the chosen
// quote with embedded quotes doubled.
func FuzzPgAttributesQuoting(f *testing.F) {
	for _, s := range []string{"a", "a'b", `a"b`, "x[0]", "[1]", "-5", ""} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		for _, q := range []string{"'", `"`} {
			for _, op := range PgAttributes([]string{in}, q) {
				if intRe.MatchString(op) {
					continue
				}
				if len(op) < 2 || op[0] != q[0] || op[len(op)-1] != q[0] {
					t.Fatalf("operand %q for %q is not wrapped in %s", op, in, q)
				}
				if strings.Count(strings.ReplaceAll(op[1:len(op)-1], q+q, ""), q) != 0 {
					t.Fatalf("operand %q for %q has an unescaped %s", op, in, q)
				}
			}
		}
	})
}
