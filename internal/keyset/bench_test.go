package keyset

import "testing"

func BenchmarkEncode(b *testing.B) {
	b.ReportAllocs()
	for range b.N {
		if _, err := Encode(int64(123456789)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	tok, err := Encode("some-cursor-value")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for range b.N {
		if _, err := Decode(tok); err != nil {
			b.Fatal(err)
		}
	}
}
