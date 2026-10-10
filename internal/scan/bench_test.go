package scan

import (
	"database/sql/driver"
	"fmt"
	"reflect"
	"testing"
)

func benchRows(n int) [][]driver.Value {
	out := make([][]driver.Value, n)
	for i := range out {
		out[i] = row{int64(i), fmt.Sprintf("user%d", i), "u@x", int64(30)}
	}
	return out
}

func BenchmarkSliceStructs(b *testing.B) {
	type user struct {
		ID       int64
		UserName string
		Email    string
		Age      int
	}
	cols := []string{"id", "user_name", "email", "age"}
	data := benchRows(100)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer() // opening the fake result set is setup, not the scan
		rs := result(b, cols, data...)
		b.StartTimer()
		var got []user
		if err := Slice(rs, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSliceMaps(b *testing.B) {
	cols := []string{"id", "user_name", "email", "age"}
	data := benchRows(100)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer() // opening the fake result set is setup, not the scan
		rs := result(b, cols, data...)
		b.StartTimer()
		var got []map[string]any
		if err := Slice(rs, &got); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFields(b *testing.B) {
	type user struct {
		ID    int64  `db:"id"`
		Name  string `db:"name"`
		Email string `db:"email"`
	}
	t := reflect.TypeFor[user]()
	b.ReportAllocs()
	for b.Loop() {
		_ = Fields(t)
	}
}
