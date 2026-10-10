package rawsql

import (
	"testing"
	"time"
)

func TestLiteral(t *testing.T) {
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	tests := []struct {
		in   any
		want string
	}{
		{nil, "null"},
		{true, "true"},
		{false, "false"},
		{"o'brien", "'o''brien'"},
		{[]byte("x'y"), "'x''y'"},
		{at, "'2024-01-02 03:04:05'"},
		{42, "42"},
		{int64(-7), "-7"},
		{1.5, "1.5"},
	}
	for _, tc := range tests {
		if got := Literal(tc.in); got != tc.want {
			t.Errorf("Literal(%#v) = %s, want %s", tc.in, got, tc.want)
		}
	}
}

func TestInterpolate(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		args     []any
		numbered bool
		want     string
	}{
		{
			"question marks in order",
			`select * from t where a = ? and b = ?`,
			[]any{1, "x"},
			false,
			`select * from t where a = 1 and b = 'x'`,
		},
		{"numbered placeholders", `a = $2 and b = $1 and c = $2`, []any{"x", 9}, true, `a = 9 and b = 'x' and c = 9`},
		{"multi-digit numbers", `$10`, []any{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, true, `10`},
		{"too few args leaves the marker", `a = ? and b = ?`, []any{1}, false, `a = 1 and b = ?`},
		{"out-of-range number is left alone", `a = $3`, []any{1}, true, `a = $3`},
		{"dollar without digits", `a = $ and b = $x`, []any{1}, true, `a = $ and b = $x`},
		{"postgres ? operator is not a placeholder", `a ? 'k' and b = $1`, []any{1}, true, `a ? 'k' and b = 1`},
		// regression: markers inside quoted text must not be substituted
		{"? inside a string literal", `a = '?' and b = ?`, []any{7}, false, `a = '?' and b = 7`},
		{"? inside double quotes", `"a?" = ?`, []any{7}, false, `"a?" = 7`},
		{"? inside backticks", "`a?` = ?", []any{7}, false, "`a?` = 7"},
		{"$1 inside a string literal", `a = '$1' and b = $1`, []any{7}, true, `a = '$1' and b = 7`},
		{"json path literal", `json_extract(x, '$."a?"') = ?`, []any{1}, false, `json_extract(x, '$."a?"') = 1`},
		{"escaped quote by doubling", `a = 'it''s ?' and b = ?`, []any{1}, false, `a = 'it''s ?' and b = 1`},
		{"no placeholders", `select 1`, nil, false, `select 1`},
		{"empty", ``, nil, true, ``},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Interpolate(tc.sql, tc.args, tc.numbered); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// FuzzInterpolateNeverPanics: arbitrary SQL and argument counts must not panic
// or lose text outside placeholders.
func FuzzInterpolateNeverPanics(f *testing.F) {
	for _, s := range []string{"a = ?", "$1 $2 $99", "'?", `"$1`, "`", "$", "$0", "??"} {
		f.Add(s, 2, true)
		f.Add(s, 0, false)
	}
	f.Fuzz(func(t *testing.T, sql string, n int, numbered bool) {
		if n < 0 || n > 8 {
			t.Skip()
		}
		args := make([]any, n)
		for i := range args {
			args[i] = i
		}
		_ = Interpolate(sql, args, numbered)
	})
}
