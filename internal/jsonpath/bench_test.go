package jsonpath

import "testing"

func BenchmarkSplitKey(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_, _ = SplitKey("u.meta->x[0]->y")
	}
}

func BenchmarkPath(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = Path("a->b->c")
	}
}

func BenchmarkPgAttributes(b *testing.B) {
	path := []string{"a", "b", "c"}
	b.ReportAllocs()
	for b.Loop() {
		_ = PgAttributes(path, "'")
	}
}
