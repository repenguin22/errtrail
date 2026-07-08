package errtrail

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"
)

func TestCodeOfNil(t *testing.T) {
	if CodeOf(nil) != OK {
		t.Errorf("CodeOf(nil) = %v, want OK", CodeOf(nil))
	}
}

func TestCodeOfPlainError(t *testing.T) {
	if got := CodeOf(errors.New("x")); got != Unknown {
		t.Errorf("CodeOf(plain) = %v, want Unknown", got)
	}
}

func TestCodeOfWrapDelegatesInner(t *testing.T) {
	inner := New(NotFound, "not found")
	outer := Wrap(inner, "outer") // outer has no code set
	if got := CodeOf(outer); got != NotFound {
		t.Errorf("CodeOf = %v, want NotFound", got)
	}
}

func TestCodeOfOuterWins(t *testing.T) {
	inner := New(NotFound, "not found")
	outer := Wrap(inner, "outer").WithCode(Internal)
	if got := CodeOf(outer); got != Internal {
		t.Errorf("CodeOf = %v, want Internal (outer)", got)
	}
}

func TestCodeOfAllUnset(t *testing.T) {
	base := errors.New("x")
	e := Wrap(base, "a") // no code set, and the inner error is a plain error
	if got := CodeOf(e); got != Unknown {
		t.Errorf("CodeOf = %v, want Unknown", got)
	}
}

func TestIsRetryable(t *testing.T) {
	if IsRetryable(nil) {
		t.Error("IsRetryable(nil) = true, want false")
	}
	if IsRetryable(errors.New("plain")) {
		t.Error("IsRetryable(plain) = true, want false (Unknown)")
	}
	if !IsRetryable(New(Unavailable, "down")) {
		t.Error("IsRetryable(Unavailable) = false, want true")
	}
	// The wrap chain inherits the inner code.
	if !IsRetryable(Wrap(New(Unavailable, "down"), "calling backend")) {
		t.Error("IsRetryable(wrapped Unavailable) = false, want true")
	}
	// An outer WithCode overrides, consistent with CodeOf.
	overridden := Wrap(New(Unavailable, "down"), "gave up").WithCode(Internal)
	if IsRetryable(overridden) {
		t.Error("IsRetryable(overridden to Internal) = true, want false")
	}
}

func TestIsRetryableCustomCode(t *testing.T) {
	const transientDep Code = 104
	Register(transientDep, "TRANSIENT_DEP", 503, 14, Retryable())
	t.Cleanup(func() { unregister(transientDep, "TRANSIENT_DEP") })

	if !IsRetryable(New(transientDep, "dep flapped")) {
		t.Error("IsRetryable(custom retryable) = false, want true")
	}
}

func TestPublicMessageFound(t *testing.T) {
	inner := New(NotFound, "internal detail").WithPublic("User not found")
	outer := Wrap(inner, "svc")
	if got := PublicMessage(outer); got != "User not found" {
		t.Errorf("PublicMessage = %q", got)
	}
}

func TestPublicMessageFallback(t *testing.T) {
	e := New(NotFound, "internal detail") // public not set
	if got := PublicMessage(e); got != "Not Found" {
		t.Errorf("PublicMessage = %q, want %q (http.StatusText)", got, "Not Found")
	}
}

func TestPublicMessageNeverLeaksInternal(t *testing.T) {
	e := New(Internal, "db password = hunter2")
	got := PublicMessage(e)
	if got == "db password = hunter2" {
		t.Error("PublicMessage leaked internal message")
	}
	if got != "Internal Server Error" {
		t.Errorf("PublicMessage = %q", got)
	}
}

func TestPublicMessageNil(t *testing.T) {
	if PublicMessage(nil) != "" {
		t.Error("PublicMessage(nil) should be empty")
	}
}

func TestTraceOrder(t *testing.T) {
	inner := New(NotFound, "get")   // where it originated
	outer := Wrap(inner, "service") // outermost wrap
	frames := Trace(outer)
	if len(frames) != 2 {
		t.Fatalf("len(frames) = %d, want 2", len(frames))
	}
	// Ordered outermost to innermost.
	if frames[0].Msg != "service" {
		t.Errorf("frames[0].Msg = %q, want service", frames[0].Msg)
	}
	if frames[1].Msg != "get" {
		t.Errorf("frames[1].Msg = %q, want get", frames[1].Msg)
	}
}

func TestTraceNoErrorType(t *testing.T) {
	if Trace(errors.New("x")) != nil {
		t.Error("Trace(plain) should be nil")
	}
}

func TestJoinDepthFirst(t *testing.T) {
	a := New(NotFound, "a")
	b := New(Internal, "b")
	joined := errors.Join(a, b)

	// CodeOf visits the first branch first, picking the first non-OK code.
	if got := CodeOf(joined); got != NotFound {
		t.Errorf("CodeOf(join) = %v, want NotFound", got)
	}
	// Trace collects every branch.
	if got := len(Trace(joined)); got != 2 {
		t.Errorf("len(Trace(join)) = %d, want 2", got)
	}
}

func TestJoinPublicFirstBranch(t *testing.T) {
	a := New(NotFound, "a") // public not set
	b := New(Internal, "b").WithPublic("boom")
	joined := errors.Join(a, b)
	// a has no public message, so the walk moves on to b and finds "boom".
	if got := PublicMessage(joined); got != "boom" {
		t.Errorf("PublicMessage(join) = %q, want boom", got)
	}
}

func TestPublicFieldsThroughChain(t *testing.T) {
	inner := New(InvalidArgument, "bad email").WithPublicField("field", "email")
	mid := fmt.Errorf("validate: %w", inner) // survives a std fmt layer
	outer := Wrap(mid, "create user").WithPublicField("request_id", "r-1")

	got := PublicFields(outer)
	if len(got) != 2 {
		t.Fatalf("fields = %v, want 2 entries", got)
	}
	if got["field"] != "email" || got["request_id"] != "r-1" {
		t.Errorf("fields = %v", got)
	}
}

func TestPublicFieldsOutermostWins(t *testing.T) {
	inner := New(InvalidArgument, "x").WithPublicField("field", "inner")
	outer := Wrap(inner, "y").WithPublicField("field", "outer")
	if got := PublicFields(outer); got["field"] != "outer" {
		t.Errorf(`fields["field"] = %v, want "outer"`, got["field"])
	}
}

func TestPublicFieldsNone(t *testing.T) {
	if PublicFields(nil) != nil {
		t.Error("PublicFields(nil) should be nil")
	}
	if PublicFields(errors.New("plain")) != nil {
		t.Error("PublicFields(plain) should be nil")
	}
	if PublicFields(New(Internal, "no fields")) != nil {
		t.Error("PublicFields(no fields) should be nil")
	}
}

func TestPublicFieldsNeverIncludesAttrs(t *testing.T) {
	e := New(Internal, "x").With(slog.String("secret", "hunter2"))
	if got := PublicFields(e); got != nil {
		t.Errorf("attrs leaked into public fields: %v", got)
	}
}

// typedNilError returns a non-nil error interface holding a nil *Error — the
// classic Go footgun, as produced by a function declared to return error that
// returns a nil concrete pointer. Going through a function boundary keeps the
// concrete type out of the caller's flow analysis (so it models real code
// rather than a comparison the compiler can fold away).
func typedNilError() error {
	var e *Error
	return e
}

func TestInspectTypedNilError(t *testing.T) {
	// The inspection functions must not panic on a typed-nil *Error.
	err := typedNilError()
	if err == nil {
		t.Fatal("interface should be non-nil")
	}

	if got := CodeOf(err); got != Unknown {
		t.Errorf("CodeOf(typed-nil) = %v, want Unknown", got)
	}
	if got := PublicMessage(err); got != "Internal Server Error" {
		t.Errorf("PublicMessage(typed-nil) = %q, want %q", got, "Internal Server Error")
	}
	if got := Trace(err); got != nil {
		t.Errorf("Trace(typed-nil) = %v, want nil", got)
	}
	if got := Attrs(err); got != nil {
		t.Errorf("Attrs(typed-nil) = %v, want nil", got)
	}
}

func TestInspectTypedNilNested(t *testing.T) {
	// A real *Error whose cause is a typed-nil *Error must also be safe: the
	// walk reaches the nil one and must skip it rather than dereference it.
	var inner *Error
	outer := Wrap(error(inner), "outer").WithCode(NotFound)

	if got := CodeOf(outer); got != NotFound {
		t.Errorf("CodeOf = %v, want NotFound", got)
	}
	// Only the outer (real) *Error contributes a frame.
	if got := len(Trace(outer)); got != 1 {
		t.Errorf("len(Trace) = %d, want 1", got)
	}
}

func TestWalkThroughStdFmtChain(t *testing.T) {
	inner := New(NotFound, "inner")
	mid := fmt.Errorf("mid: %w", inner)
	outer := Wrap(mid, "outer")
	if got := CodeOf(outer); got != NotFound {
		t.Errorf("CodeOf through fmt chain = %v, want NotFound", got)
	}
}
