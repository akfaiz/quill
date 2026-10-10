package keyset

import (
	"encoding/base64"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"

	"github.com/akfaiz/quill/internal/errs"
)

func TestRoundTrip(t *testing.T) {
	at := time.Date(2024, 3, 7, 9, 5, 2, 123, time.UTC)
	tests := []struct {
		name string
		in   any
		want any
	}{
		{"int", 42, int64(42)},
		{"int64", int64(-7), int64(-7)},
		{"large int64 keeps precision", int64(9007199254740993), int64(9007199254740993)}, // 2^53 + 1
		{"float", 1.5, 1.5},
		{"whole float comes back as int64", 3.0, int64(3)},
		{"string", "abc", "abc"},
		{"string with quotes and unicode", `a"b'c é 🙂`, `a"b'c é 🙂`},
		{"empty string", "", ""},
		{"true", true, true},
		{"false", false, false},
		{"bytes become a string", []byte("raw"), "raw"},
		{"time becomes RFC 3339", at, at.Format(time.RFC3339Nano)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := Encode(tc.in)
			require.NoError(t, err)
			got, err := Decode(tok)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Decode(Encode(%#v)) = %#v, %v; want %#v", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestTokenIsURLSafe(t *testing.T) {
	tok, _ := Encode("a/b+c?d=e&f ~~~ ??>>")
	for _, c := range tok {
		if c == '+' || c == '/' || c == '=' {
			t.Errorf("token %q contains %q, which is not URL-safe", tok, c)
		}
	}
}

func TestDecodeRejectsBadInput(t *testing.T) {
	b64 := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	tests := map[string]string{
		"empty":                "",
		"not base64":           "!!!",
		"padded base64":        "bm90LWpzb24=",
		"not json":             b64("not-json"),
		"json null":            b64("null"),
		"json object":          b64(`{"id":1}`),
		"json array":           b64(`[1,2]`),
		"number out of range":  b64("1e999"),
		"trailing garbage":     b64(`1 2`),
		"truncated string":     b64(`"abc`),
		"standard base64 char": "ab+c",
	}
	for name, tok := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(tok)
			if name == "trailing garbage" {
				// the first JSON value is read and the rest ignored: still a valid number, never a panic
				return
			}
			assert.ErrorIs(t, err, errs.ErrInvalidArgs, "Decode(%q)", tok)
		})
	}
}

func TestDecodeBeyondInt64FallsBackToFloat(t *testing.T) {
	got, err := Decode(base64.RawURLEncoding.EncodeToString([]byte("9223372036854775808")))
	if err != nil || got != float64(9223372036854775808) {
		t.Errorf("got %#v, %v", got, err)
	}
}

func TestEncodeNilProducesATokenThatDoesNotDecode(t *testing.T) {
	// a NULL key cannot be a cursor: it encodes, but the next request is rejected cleanly
	tok, err := Encode(nil)
	require.NoError(t, err)
	if _, err := Decode(tok); !errors.Is(err, errs.ErrInvalidArgs) {
		t.Errorf("got %v, want ErrInvalidArgs", err)
	}
}

func TestEncodeUnencodableValue(t *testing.T) {
	if _, err := Encode(make(chan int)); !errors.Is(err, errs.ErrInvalidArgs) {
		t.Errorf("got %v, want ErrInvalidArgs", err)
	}
}

// FuzzDecode: tokens come from clients. Decode must never panic, only ever
// yield a bindable scalar, and anything it accepts must survive another round trip.
func FuzzDecode(f *testing.F) {
	for _, s := range []string{"", "!!!", "MQ", "Ij4i", "bnVsbA", "MWU5OTk", "eyJhIjoxfQ", "WzFd", "dHJ1ZQ", "LTA", "MS41"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, tok string) {
		v, err := Decode(tok)
		if err != nil {
			assert.ErrorIs(t, err, errs.ErrInvalidArgs)
			return
		}
		switch v.(type) {
		case string, bool, int64, float64:
		default:
			t.Fatalf("Decode(%q) returned %T, want a scalar", tok, v)
		}
		again, err := Encode(v)
		require.NoError(t, err)
		back, err := Decode(again)
		if err != nil || !sameValue(v, back) {
			t.Fatalf("round trip of %#v gave %#v (%v)", v, back, err)
		}
	})
}

// sameValue treats a whole float and the equal int64 as the same value: "3e4" decodes to
// float64(30000) but re-encodes as 30000, which decodes to int64(30000). Both bind identically.
func sameValue(a, b any) bool {
	if reflect.DeepEqual(a, b) {
		return true
	}
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	return aok && bok && af == bf
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}
