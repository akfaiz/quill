package rawsql

import "testing"

func BenchmarkInterpolate(b *testing.B) {
	const q = `select * from "users" where "id" = $1 and "name" = $2 and "bio" <> '?' and "age" > $3`
	args := []any{42, "o'brien", 18}
	b.ReportAllocs()
	for range b.N {
		_ = Interpolate(q, args, true)
	}
}

func BenchmarkLiteral(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = Literal("o'brien")
	}
}
