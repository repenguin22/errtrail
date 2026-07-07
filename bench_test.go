package errtrail

import (
	"errors"
	"fmt"
	"testing"
)

// sink はエスケープ解析による割り当て消去を防ぎ、実際のヒープ確保を計測させる。
var sink error

func BenchmarkNew(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = New(NotFound, "not found")
	}
}

func BenchmarkWrap(b *testing.B) {
	base := errors.New("base")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = Wrap(base, "layer")
	}
}

func BenchmarkWrapChain3(b *testing.B) {
	base := errors.New("base")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sink = Wrap(Wrap(Wrap(base, "a"), "b"), "c")
	}
}

func BenchmarkFormatPlusV(b *testing.B) {
	err := Wrap(Wrap(New(NotFound, "get"), "b"), "c")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = fmt.Sprintf("%+v", err)
	}
}
