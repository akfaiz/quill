package rowmap

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type plain struct {
	S   string
	I   int
	I8  int8
	U   uint
	F   float64
	F32 float32
	B   bool
	T   time.Time
}

func scanPlain(t *testing.T, vals row) (plain, error) {
	t.Helper()
	cols := []string{"s", "i", "i8", "u", "f", "f32", "b", "t"}
	var got []plain
	err := Slice(result(t, cols, vals), &got)
	if len(got) == 0 {
		return plain{}, err
	}
	return got[0], err
}

func TestScannerNullLeavesZero(t *testing.T) {
	got, err := scanPlain(t, row{nil, nil, nil, nil, nil, nil, nil, nil})
	require.NoError(t, err)
	assert.Equal(t, plain{}, got)
}

func TestScannerNativeAndTextValues(t *testing.T) {
	now := time.Date(2026, 10, 11, 1, 2, 3, 0, time.UTC)
	native, err := scanPlain(t, row{"a", int64(5), int64(-3), int64(7), 1.5, 2.5, true, now})
	require.NoError(t, err)
	assert.Equal(t, plain{S: "a", I: 5, I8: -3, U: 7, F: 1.5, F32: 2.5, B: true, T: now}, native)

	// text-protocol drivers (MySQL) return []byte for every column
	text, err := scanPlain(t, row{
		[]byte("b"), []byte("42"), []byte("-8"), []byte("9"), []byte("0.25"), []byte("0.5"), []byte("1"), now,
	})
	require.NoError(t, err)
	assert.Equal(t, plain{S: "b", I: 42, I8: -8, U: 9, F: 0.25, F32: 0.5, B: true, T: now}, text)

	mixed, err := scanPlain(t, row{int64(12), "7", float64(3), float64(4), int64(2), int64(1), int64(0), now})
	require.NoError(t, err)
	assert.Equal(t, plain{S: "12", I: 7, I8: 3, U: 4, F: 2, F32: 1, B: false, T: now}, mixed)
}

func TestScannerRejectsBadValues(t *testing.T) {
	ok := row{"a", int64(1), int64(1), int64(1), 1.0, 1.0, true, time.Time{}}
	with := func(i int, v any) row {
		r := append(row(nil), ok...)
		r[i] = v
		return r
	}
	tests := map[string]row{
		"int8 overflow":     with(2, int64(300)),
		"negative uint":     with(3, int64(-1)),
		"text into int":     with(1, "abc"),
		"fraction into int": with(1, 3.5),
		"text into float":   with(4, "x"),
		"bad bool":          with(6, "maybe"),
		"string into time":  with(7, "2026-01-01"),
	}
	for name, r := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := scanPlain(t, r)
			assert.Error(t, err)
		})
	}
}

func TestScannerReusedAcrossRows(t *testing.T) {
	cols := []string{"s", "i"}
	var got []struct {
		S string
		I int
	}
	err := Slice(result(t, cols, row{"a", int64(1)}, row{nil, nil}, row{"c", int64(3)}), &got)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "a", got[0].S)
	assert.Zero(t, got[1])
	assert.Equal(t, 3, got[2].I)
}

func TestSlicePointerElementsAndScalars(t *testing.T) {
	var ptrs []*plain
	require.NoError(t, Slice(result(t, []string{"s", "i"}, row{"a", int64(1)}, row{"b", int64(2)}), &ptrs))
	require.Len(t, ptrs, 2)
	assert.NotSame(t, ptrs[0], ptrs[1])
	assert.Equal(t, "b", ptrs[1].S)

	var names []string
	require.NoError(t, Slice(result(t, []string{"s"}, row{"x"}, row{nil}, row{[]byte("z")}), &names))
	assert.Equal(t, []string{"x", "", "z"}, names)
}

func TestSliceLeavesDestOnError(t *testing.T) {
	got := []plain{{S: "keep"}}
	err := Slice(result(t, []string{"i"}, row{int64(1)}, row{"abc"}), &got)
	require.Error(t, err)
	assert.Equal(t, []plain{{S: "keep"}}, got)
}

func TestAsString(t *testing.T) {
	now := time.Date(2026, 10, 11, 1, 2, 3, 0, time.UTC)
	assert.Equal(t, "x", asString("x"))
	assert.Equal(t, "b", asString([]byte("b")))
	assert.Equal(t, "2026-10-11T01:02:03Z", asString(now))
	assert.Equal(t, "7", asString(int64(7)))
	assert.Equal(t, "1.5", asString(1.5))
	assert.Equal(t, "true", asString(true))
	assert.Equal(t, "<nil>", asString(nil))
}
