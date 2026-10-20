package jsonpath

import "testing"

func BenchmarkSplitKey(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_, _ = SplitKey("u.meta->x[0]->y")
	}
}

func BenchmarkPath(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		_ = Path("a->b->c")
	}
}

func BenchmarkPgAttributes(b *testing.B) {
	path := []string{"a", "b", "c"}
	b.ReportAllocs()
	for range b.N {
		_ = PgAttributes(path, "'")
	}
}
