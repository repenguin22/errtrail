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
	// errors.As should work even through a mixed chain of
	// errtrail -> fmt.Errorf(%w) -> errtrail.
	inner := New(NotFound, "inner")
	mid := fmt.Errorf("mid: %w", inner)
	outer := Wrap(mid, "outer")

	var target *Error
	if !errors.As(outer, &target) {
		t.Fatal("errors.As should find *Error")
	}
	// As should find the outermost *Error (outer).
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
	// Derive two errors from e; appending to one must not leak into the other.
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

func TestImmutabilityPublicFieldsNoSharing(t *testing.T) {
	e := New(InvalidArgument, "x").WithPublicField("a", 1)
	// Derive two errors from e; appending to one must not leak into the other.
	b := e.WithPublicField("b", 2)
	c := e.WithPublicField("c", 3)

	if got := PublicFields(e); len(got) != 1 {
		t.Errorf("e fields = %v, want 1 entry", got)
	}
	if got := PublicFields(b); len(got) != 2 || got["b"] != 2 {
		t.Errorf("b fields = %v", got)
	}
	if got := PublicFields(c); len(got) != 2 || got["c"] != 3 {
		t.Errorf("c fields = %v", got)
	}
	if got := PublicFields(b); got["c"] != nil {
		t.Errorf("c leaked into b: %v", got)
	}
}

func TestImmutabilityWithoutPublic(t *testing.T) {
	inner := New(NotFound, "x").WithPublic("inner")
	e := Wrap(inner, "y")
	e2 := e.WithoutPublic()
	if e.noPublicBelow {
		t.Error("original barrier flag mutated")
	}
	if !e2.noPublicBelow {
		t.Error("copy barrier flag not set")
	}
	// The original keeps exposing the inner public message.
	if got := PublicMessage(e); got != "inner" {
		t.Errorf("original PublicMessage = %q, want inner", got)
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
	if e.WithoutPublic() != nil {
		t.Error("nil.WithoutPublic should be nil")
	}
	if e.With(slog.Int("a", 1)) != nil {
		t.Error("nil.With should be nil")
	}
	if e.WithPublicField("a", 1) != nil {
		t.Error("nil.WithPublicField should be nil")
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
