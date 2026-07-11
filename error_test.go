package errtrail

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"
	"unsafe"
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

func TestWrapfFormatsMessage(t *testing.T) {
	base := errors.New("boom")
	e := Wrapf(base, "attempt %d", 3)
	if got := e.Error(); got != "attempt 3: boom" {
		t.Errorf("Error() = %q, want %q", got, "attempt 3: boom")
	}
	if !errors.Is(e, base) {
		t.Error("errors.Is should find base through Unwrap")
	}
}

// newSkipFactory stands in for an application error factory: the frame it
// records must point at ITS caller, not at this function.
func newSkipFactory(msg string) *Error {
	return NewSkip(1, NotFound, msg)
}

func wrapSkipFactory(err error, msg string) *Error {
	return WrapSkip(1, err, msg)
}

func TestSkipZeroEqualsPlainConstructors(t *testing.T) {
	direct := Trace(New(NotFound, "x"))[0].Function
	if got := Trace(NewSkip(0, NotFound, "x"))[0].Function; got != direct {
		t.Errorf("NewSkip(0) frame = %q, want %q (same as New)", got, direct)
	}
	base := errors.New("base")
	directW := Trace(Wrap(base, "x"))[0].Function
	if got := Trace(WrapSkip(0, base, "x"))[0].Function; got != directW {
		t.Errorf("WrapSkip(0) frame = %q, want %q (same as Wrap)", got, directW)
	}
}

func TestSkipOnePointsAtFactoryCaller(t *testing.T) {
	if fn := Trace(newSkipFactory("missing"))[0].Function; !strings.Contains(fn, "TestSkipOnePointsAtFactoryCaller") {
		t.Errorf("NewSkip(1) frame = %q, want the factory's caller (this test)", fn)
	}
	w := wrapSkipFactory(errors.New("base"), "ctx")
	if fn := Trace(w)[0].Function; !strings.Contains(fn, "TestSkipOnePointsAtFactoryCaller") {
		t.Errorf("WrapSkip(1) frame = %q, want the factory's caller (this test)", fn)
	}
}

func TestWrapSkipNilReturnsNil(t *testing.T) {
	if WrapSkip(1, nil, "x") != nil {
		t.Error("WrapSkip(nil) should be nil")
	}
}

func TestSkipNegativePanics(t *testing.T) {
	cases := map[string]func(){
		"NewSkip":  func() { NewSkip(-1, Internal, "x") },
		"WrapSkip": func() { WrapSkip(-1, errors.New("b"), "x") },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected a panic on negative skip")
				}
			}()
			fn()
		})
	}
}

func TestSkipPastStackTopRecordsUnknown(t *testing.T) {
	if fn := Trace(NewSkip(1000, Internal, "x"))[0].Function; fn != "unknown" {
		t.Errorf("Function = %q, want unknown", fn)
	}
}

func TestSkipOverflowRecordsUnknown(t *testing.T) {
	// A skip large enough that 3+skip overflows int must still hit the
	// documented "unknown" fallback, not wrap around into a bogus frame
	// somewhere near the top of the stack (Round 7 review finding).
	if fn := Trace(NewSkip(math.MaxInt, Internal, "x"))[0].Function; fn != "unknown" {
		t.Errorf("NewSkip(MaxInt) Function = %q, want unknown", fn)
	}
	if fn := Trace(WrapSkip(math.MaxInt, errors.New("x"), "y"))[0].Function; fn != "unknown" {
		t.Errorf("WrapSkip(MaxInt) Function = %q, want unknown", fn)
	}
}

func TestWithRetryDelay(t *testing.T) {
	base := New(ResourceExhausted, "bucket empty")
	e := base.WithRetryDelay(10 * time.Second)
	if d, ok := LookupRetryDelay(e); !ok || d != 10*time.Second {
		t.Errorf("LookupRetryDelay = %v, %v; want 10s, true", d, ok)
	}
	// Immutability: the original is untouched.
	if _, ok := LookupRetryDelay(base); ok {
		t.Error("WithRetryDelay must not mutate the receiver")
	}
}

func TestWithRetryDelayNonPositiveIsNoOp(t *testing.T) {
	// A non-positive delay carries no recommendation and leaves the error
	// unchanged — including an already-set delay (the input is a computed
	// value; zero must not clear an earlier hint by accident).
	e := New(ResourceExhausted, "bucket empty")
	if got := e.WithRetryDelay(0); got != e {
		t.Error("WithRetryDelay(0) should return the receiver unchanged")
	}
	if got := e.WithRetryDelay(-time.Second); got != e {
		t.Error("WithRetryDelay(negative) should return the receiver unchanged")
	}
	withDelay := e.WithRetryDelay(10 * time.Second)
	if d, _ := LookupRetryDelay(withDelay.WithRetryDelay(0)); d != 10*time.Second {
		t.Errorf("WithRetryDelay(0) after a set delay = %v, want 10s kept", d)
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

func TestImmutabilityFieldViolationsNoSharing(t *testing.T) {
	e := New(InvalidArgument, "x").WithFieldViolation("a", "1")
	// Derive two errors from e; appending to one must not leak into the other.
	b := e.WithFieldViolation("b", "2")
	c := e.WithFieldViolation("c", "3")

	if got := FieldViolations(e); len(got) != 1 {
		t.Errorf("e violations = %v, want 1 entry", got)
	}
	if got := FieldViolations(b); len(got) != 2 || got[1].Field != "b" {
		t.Errorf("b violations = %v", got)
	}
	if got := FieldViolations(c); len(got) != 2 || got[1].Field != "c" {
		t.Errorf("c violations = %v", got)
	}
}

func TestNilReceiverSafety(t *testing.T) {
	var e *Error
	if e.WithCode(Internal) != nil {
		t.Error("nil.WithCode should be nil")
	}
	if e.WithFieldViolation("a", "b") != nil {
		t.Error("nil.WithFieldViolation should be nil")
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
	if e.WithRetryDelay(time.Second) != nil {
		t.Error("nil.WithRetryDelay should be nil")
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

func TestErrorStructSize(t *testing.T) {
	// noPublicBelow is placed right after code (not trailing the struct) so
	// it fills Code's padding byte instead of adding one, keeping
	// construction in the 144-byte allocator size class on 64-bit platforms
	// rather than spilling into the 160-byte one (Round 7 review finding).
	// Pin the raw size, since a future field reorder or addition could
	// regress it silently. On 32-bit (uintptr = 4 bytes; int64 fields like
	// retryDelay align to 4, not 8, there) the layout comes out to 80 bytes
	// instead — a second Round 7 finding on this same test, confirmed by
	// hand-computing the 386 layout since there's no 32-bit CI runner.
	want := uintptr(144)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want = 80
	}
	if got := unsafe.Sizeof(Error{}); got != want {
		t.Errorf("unsafe.Sizeof(Error{}) = %d, want %d", got, want)
	}
}
