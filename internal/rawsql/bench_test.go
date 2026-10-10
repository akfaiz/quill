package rawsql

import "testing"

func BenchmarkInterpolate(b *testing.B) {
	const q = `select * from "users" where "id" = $1 and "name" = $2 and "bio" <> '?' and "age" > $3`
	args := []any{42, "o'brien", 18}
	b.ReportAllocs()
	for b.Loop() {
		_ = Interpolate(q, args, true)
	}
}

func BenchmarkLiteral(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = Literal("o'brien")
	}
}
