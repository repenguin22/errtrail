package errtrail

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"
)

func TestNewAndError(t *testing.T) {
	e := New(NotFound, "user not found")
	if e.Error() != "user not found" {
		t.Errorf("Error() = %q", e.Error())
	}
	if e.code != NotFound {
		t.Errorf("code = %v", e.code)
	}
	if e.pc == 0 {
		t.Error("expected a recorded frame")
	}
}

func TestNewf(t *testing.T) {
	e := Newf(NotFound, "user %d not found", 42)
	if e.Error() != "user 42 not found" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestWrapMessageConcatenation(t *testing.T) {
	base := errors.New("sql: no rows")
	e := Wrap(base, "query user")
	if e.Error() != "query user: sql: no rows" {
		t.Errorf("Error() = %q", e.Error())
	}
}

func TestWrapEmptyMsgOmitsColon(t *testing.T) {
	base := errors.New("boom")
	e := Wrap(base, "")
	if e.Error() != "boom" {
		t.Errorf("Error() = %q, want %q", e.Error(), "boom")
	}
}

func TestWrapNilReturnsNil(t *testing.T) {
	if Wrap(nil, "x") != nil {
		t.Error("Wrap(nil) should be nil")
	}
	if Wrapf(nil, "x %d", 1) != nil {
		t.Error("Wrapf(nil) should be nil")
	}
}

func TestUnwrapAndErrorsIs(t *testing.T) {
	sentinel := errors.New("sentinel")
	e := Wrap(sentinel, "layer")
	if !errors.Is(e, sentinel) {
		t.Error("errors.Is should find sentinel through Unwrap")
	}
}

func TestErrorsAsThroughStdChain(t *testing.T) {
	// errtrail → fmt.Errorf(%w) → errtrail の混在チェーンでも As が通ること。
	inner := New(NotFound, "inner")
	mid := fmt.Errorf("mid: %w", inner)
	outer := Wrap(mid, "outer")

	var target *Error
	if !errors.As(outer, &target) {
		t.Fatal("errors.As should find *Error")
	}
	// As は最も外側の *Error(outer)を拾う。
	if target != outer {
		t.Errorf("As found %v, want outer", target)
	}
}

func TestImmutabilityWithCode(t *testing.T) {
	e := New(NotFound, "x")
	e2 := e.WithCode(Internal)
	if e.code != NotFound {
		t.Error("original code mutated")
	}
	if e2.code != Internal {
		t.Error("copy code not set")
	}
}

func TestImmutabilityWithPublic(t *testing.T) {
	e := New(NotFound, "x")
	e2 := e.WithPublic("hello")
	if e.public != "" {
		t.Error("original public mutated")
	}
	if e2.public != "hello" {
		t.Error("copy public not set")
	}
}

func TestImmutabilityAttrsNoSharing(t *testing.T) {
	e := New(Internal, "x").With(slog.String("a", "1"))
	// e から 2 つ派生させ、片方の append がもう片方に漏れないこと。
	b := e.With(slog.String("b", "2"))
	c := e.With(slog.String("c", "3"))

	if len(Attrs(e)) != 1 {
		t.Errorf("e attrs = %d, want 1", len(Attrs(e)))
	}
	if got := attrKeys(b); !equalStrs(got, []string{"a", "b"}) {
		t.Errorf("b attrs = %v", got)
	}
	if got := attrKeys(c); !equalStrs(got, []string{"a", "c"}) {
		t.Errorf("c attrs = %v", got)
	}
}

func TestNilReceiverSafety(t *testing.T) {
	var e *Error
	if e.WithCode(Internal) != nil {
		t.Error("nil.WithCode should be nil")
	}
	if e.WithPublic("x") != nil {
		t.Error("nil.WithPublic should be nil")
	}
	if e.With(slog.Int("a", 1)) != nil {
		t.Error("nil.With should be nil")
	}
	if e.Error() != "<nil>" {
		t.Errorf("nil.Error() = %q, want <nil>", e.Error())
	}
	if e.Unwrap() != nil {
		t.Error("nil.Unwrap should be nil")
	}
}

func attrKeys(err error) []string {
	attrs := Attrs(err)
	keys := make([]string, len(attrs))
	for i, a := range attrs {
		keys[i] = a.Key
	}
	return keys
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
