package errtrail

import (
	"errors"
	"fmt"
	"testing"
)

// sink prevents escape analysis from eliding the allocations, so the
// benchmarks measure real heap usage.
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
