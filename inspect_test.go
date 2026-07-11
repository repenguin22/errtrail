package errtrail

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"
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

func TestPublicMessageCodeNameFallback(t *testing.T) {
	// http.StatusText knows nothing about Canceled's 499 — the second-level
	// fallback hands the client the code name, never "".
	if got := PublicMessage(New(Canceled, "ctx canceled")); got != "CANCELED" {
		t.Errorf("PublicMessage(Canceled) = %q, want CANCELED", got)
	}

	// Same for a custom code on a non-standard (but in-range) HTTP status.
	const odd Code = 140
	Register(odd, "ODD_STATUS", 599, 13)
	t.Cleanup(func() { unregister(odd, "ODD_STATUS") })
	if got := PublicMessage(New(odd, "x")); got != "ODD_STATUS" {
		t.Errorf("PublicMessage(custom 599) = %q, want ODD_STATUS", got)
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

func TestLookupPublicMessage(t *testing.T) {
	if msg, ok := LookupPublicMessage(nil); ok || msg != "" {
		t.Errorf("LookupPublicMessage(nil) = %q, %v; want \"\", false", msg, ok)
	}
	if msg, ok := LookupPublicMessage(errors.New("plain")); ok || msg != "" {
		t.Errorf("LookupPublicMessage(plain) = %q, %v; want \"\", false", msg, ok)
	}
	// Unset public: no fallback, unlike PublicMessage.
	if msg, ok := LookupPublicMessage(New(NotFound, "row missing")); ok || msg != "" {
		t.Errorf("LookupPublicMessage(unset) = %q, %v; want \"\", false", msg, ok)
	}

	inner := New(NotFound, "row missing").WithPublic("User not found")
	outer := Wrap(inner, "load profile")
	if msg, ok := LookupPublicMessage(outer); !ok || msg != "User not found" {
		t.Errorf("LookupPublicMessage(chain) = %q, %v; want set message", msg, ok)
	}

	// The first (outermost) explicitly-set message wins.
	overridden := Wrap(inner, "load profile").WithPublic("Profile unavailable")
	if msg, _ := LookupPublicMessage(overridden); msg != "Profile unavailable" {
		t.Errorf("LookupPublicMessage(overridden) = %q, want outermost", msg)
	}
}

func TestLookupRetryDelay(t *testing.T) {
	if d, ok := LookupRetryDelay(nil); ok || d != 0 {
		t.Errorf("LookupRetryDelay(nil) = %v, %v; want 0, false", d, ok)
	}
	if d, ok := LookupRetryDelay(errors.New("plain")); ok || d != 0 {
		t.Errorf("LookupRetryDelay(plain) = %v, %v; want 0, false", d, ok)
	}
	if d, ok := LookupRetryDelay(New(Unavailable, "down")); ok || d != 0 {
		t.Errorf("LookupRetryDelay(unset) = %v, %v; want 0, false", d, ok)
	}

	inner := New(ResourceExhausted, "bucket empty").WithRetryDelay(10 * time.Second)
	if d, ok := LookupRetryDelay(inner); !ok || d != 10*time.Second {
		t.Errorf("LookupRetryDelay(set) = %v, %v; want 10s, true", d, ok)
	}

	// A wrap without its own delay falls through to the inner one.
	if d, _ := LookupRetryDelay(Wrap(inner, "call limiter")); d != 10*time.Second {
		t.Errorf("LookupRetryDelay(wrapped) = %v, want inner 10s", d)
	}

	// The first (outermost) delay wins.
	outer := Wrap(inner, "call limiter").WithRetryDelay(5 * time.Second)
	if d, _ := LookupRetryDelay(outer); d != 5*time.Second {
		t.Errorf("LookupRetryDelay(overridden) = %v, want outermost 5s", d)
	}

	// A Join visits its first branch first, like LookupPublicMessage.
	second := New(Unavailable, "down").WithRetryDelay(3 * time.Second)
	if d, _ := LookupRetryDelay(errors.Join(inner, second)); d != 10*time.Second {
		t.Errorf("LookupRetryDelay(join) = %v, want first branch's 10s", d)
	}
}

func TestLookupRetryDelayBarrier(t *testing.T) {
	inner := New(ResourceExhausted, "bucket empty").WithRetryDelay(10 * time.Second)

	// A delay below the barrier is not exposed.
	blocked := Wrap(inner, "reclassify").WithoutPublic()
	if d, ok := LookupRetryDelay(blocked); ok || d != 0 {
		t.Errorf("LookupRetryDelay(below barrier) = %v, %v; want 0, false", d, ok)
	}

	// The barrier node's own delay sits above the barrier and survives.
	own := Wrap(inner, "reclassify").WithoutPublic().WithRetryDelay(5 * time.Second)
	if d, ok := LookupRetryDelay(own); !ok || d != 5*time.Second {
		t.Errorf("LookupRetryDelay(own above barrier) = %v, %v; want 5s, true", d, ok)
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

func TestPublicFieldsLastWinsWithinNode(t *testing.T) {
	// Within one node the last WithPublicField wins — consistent with
	// calling WithPublic twice, where the last message wins.
	e := New(InvalidArgument, "x").
		WithPublicField("k", "first").
		WithPublicField("k", "second")
	if got := PublicFields(e); got["k"] != "second" {
		t.Errorf(`fields["k"] = %v, want "second" (last write wins)`, got["k"])
	}

	// Across nodes the outermost still overrides the inner node's last write.
	outer := Wrap(e, "y").WithPublicField("k", "outer")
	if got := PublicFields(outer); got["k"] != "outer" {
		t.Errorf(`fields["k"] = %v, want "outer" (outermost wins across nodes)`, got["k"])
	}
}

func TestPublicFieldsJoinFirstBranchWins(t *testing.T) {
	// Across Join branches, the first branch keeps the key (depth-first
	// walk order, same as CodeOf / PublicMessage).
	a := New(InvalidArgument, "a").WithPublicField("field", "email")
	b := New(InvalidArgument, "b").WithPublicField("field", "age")
	joined := errors.Join(a, b)
	if got := PublicFields(joined); got["field"] != "email" {
		t.Errorf(`fields["field"] = %v, want the first branch's "email"`, got["field"])
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

func TestFieldViolationsThroughChain(t *testing.T) {
	inner := New(InvalidArgument, "bad request").
		WithFieldViolation("email", "must be valid").
		WithFieldViolation("age", "must be >= 0")
	mid := fmt.Errorf("validate: %w", inner) // survives a std fmt layer
	outer := Wrap(mid, "create user").WithFieldViolation("request", "malformed")

	got := FieldViolations(outer)
	// A list in walk order (outermost first), nothing deduplicated.
	want := []FieldViolation{
		{Field: "request", Description: "malformed"},
		{Field: "email", Description: "must be valid"},
		{Field: "age", Description: "must be >= 0"},
	}
	if len(got) != len(want) {
		t.Fatalf("violations = %v, want %d entries", got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("violations[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestFieldViolationsJoin(t *testing.T) {
	a := New(InvalidArgument, "a").WithFieldViolation("email", "invalid")
	b := New(InvalidArgument, "b").WithFieldViolation("age", "negative")
	got := FieldViolations(errors.Join(a, b))
	if len(got) != 2 || got[0].Field != "email" || got[1].Field != "age" {
		t.Errorf("violations = %v, want both branches in branch order", got)
	}
}

func TestFieldViolationsBarrier(t *testing.T) {
	inner := New(InvalidArgument, "x").WithFieldViolation("email", "invalid")
	blocked := Wrap(inner, "reclassify").WithoutPublic()
	if got := FieldViolations(blocked); got != nil {
		t.Errorf("violations = %v, want nil below a barrier", got)
	}
	// The barrier node's own violations — and outer re-adds — still apply.
	readd := blocked.WithFieldViolation("request", "rejected")
	if got := FieldViolations(readd); len(got) != 1 || got[0].Field != "request" {
		t.Errorf("violations = %v, want only the barrier node's own", got)
	}
}

func TestFieldViolationsNone(t *testing.T) {
	if FieldViolations(nil) != nil {
		t.Error("FieldViolations(nil) should be nil")
	}
	if FieldViolations(errors.New("plain")) != nil {
		t.Error("FieldViolations(plain) should be nil")
	}
	if FieldViolations(New(Internal, "no violations")) != nil {
		t.Error("FieldViolations(no violations) should be nil")
	}
}

func TestPublicFieldsNeverIncludesAttrs(t *testing.T) {
	e := New(Internal, "x").With(slog.String("secret", "hunter2"))
	if got := PublicFields(e); got != nil {
		t.Errorf("attrs leaked into public fields: %v", got)
	}
}

func TestWithoutPublicBlocksChainBelow(t *testing.T) {
	// The motivating scenario: reclassifying NotFound -> PermissionDenied to
	// hide existence must not leak the inner public message or fields
	// through the 403.
	inner := New(NotFound, "row missing").
		WithPublic("User not found").
		WithPublicField("user_id", "42")
	outer := Wrap(inner, "reclassify lookup").WithCode(PermissionDenied).WithoutPublic()

	if msg, ok := LookupPublicMessage(outer); ok || msg != "" {
		t.Errorf("LookupPublicMessage = %q, %v; want \"\", false (blocked)", msg, ok)
	}
	// PublicMessage falls back to the new code's status text, not the inner message.
	if got := PublicMessage(outer); got != "Forbidden" {
		t.Errorf("PublicMessage = %q, want Forbidden", got)
	}
	if got := PublicFields(outer); got != nil {
		t.Errorf("PublicFields = %v, want nil (blocked)", got)
	}
	if got := CodeOf(outer); got != PermissionDenied {
		t.Errorf("CodeOf = %v, want PermissionDenied", got)
	}
}

func TestWithoutPublicReAddOutside(t *testing.T) {
	inner := New(NotFound, "x").WithPublic("inner msg").WithPublicField("k", "inner")

	// Public data on the barrier node itself still applies — the barrier
	// blocks only the chain below, so the order of WithoutPublic relative to
	// WithPublic / WithPublicField on the same node makes no difference.
	same := Wrap(inner, "y").WithoutPublic().WithPublic("outer msg").WithPublicField("k", "outer")
	if got := PublicMessage(same); got != "outer msg" {
		t.Errorf("PublicMessage = %q, want the barrier node's own message", got)
	}
	if got := PublicFields(same); got["k"] != "outer" {
		t.Errorf(`fields["k"] = %v, want "outer"`, got["k"])
	}
	reversed := Wrap(inner, "y").WithPublic("outer msg").WithoutPublic()
	if got := PublicMessage(reversed); got != "outer msg" {
		t.Errorf("PublicMessage (reversed order) = %q, want the same result", got)
	}

	// An outer wrap above the barrier contributes as usual.
	rewrapped := Wrap(Wrap(inner, "y").WithoutPublic(), "z").WithPublic("rewrapped").WithPublicField("k2", "v2")
	if got := PublicMessage(rewrapped); got != "rewrapped" {
		t.Errorf("PublicMessage = %q, want the outer wrap's message", got)
	}
	if got := PublicFields(rewrapped); len(got) != 1 || got["k2"] != "v2" {
		t.Errorf("PublicFields = %v, want only the outer wrap's field", got)
	}
}

func TestWithoutPublicOnOriginalNodeKeepsOwnData(t *testing.T) {
	// Misuse pin: WithoutPublic must be called on a fresh Wrap. Called on
	// the error value itself, that node's OWN public data sits above the
	// barrier and still reaches clients — only the chain below is blocked.
	orig := New(NotFound, "row missing").
		WithPublic("User not found").
		WithPublicField("user_id", "42").
		WithFieldViolation("user_id", "does not exist")

	misused := orig.WithCode(PermissionDenied).WithoutPublic()
	if got := PublicMessage(misused); got != "User not found" {
		t.Errorf("PublicMessage = %q, want the node's own message (misuse keeps it)", got)
	}
	if got := PublicFields(misused); got["user_id"] != "42" {
		t.Errorf("PublicFields = %v, want the node's own field", got)
	}
	if got := FieldViolations(misused); len(got) != 1 {
		t.Errorf("FieldViolations = %v, want the node's own violation", got)
	}

	// The documented form — a fresh Wrap — blocks all three channels.
	proper := Wrap(orig, "reclassify").WithCode(PermissionDenied).WithoutPublic()
	if msg, ok := LookupPublicMessage(proper); ok {
		t.Errorf("LookupPublicMessage = %q, want blocked", msg)
	}
	if got := PublicFields(proper); got != nil {
		t.Errorf("PublicFields = %v, want nil", got)
	}
	if got := FieldViolations(proper); got != nil {
		t.Errorf("FieldViolations = %v, want nil", got)
	}
}

func TestWithoutPublicInternalUnaffected(t *testing.T) {
	inner := New(NotFound, "row missing").
		WithPublic("User not found").
		With(slog.String("query", "select 1"))
	blocked := Wrap(inner, "reclassify").WithoutPublic()
	plain := Wrap(inner, "reclassify")

	if blocked.Error() != plain.Error() {
		t.Errorf("Error() = %q, want %q (unchanged)", blocked.Error(), plain.Error())
	}
	if got, want := len(Trace(blocked)), len(Trace(plain)); got != want {
		t.Errorf("len(Trace) = %d, want %d (unchanged)", got, want)
	}
	if got, want := attrKeys(blocked), attrKeys(plain); !equalStrs(got, want) {
		t.Errorf("Attrs = %v, want %v (unchanged)", got, want)
	}
	if got := CodeOf(blocked); got != NotFound {
		t.Errorf("CodeOf = %v, want NotFound (code still delegates through the barrier)", got)
	}
}

func TestWithoutPublicJoinSiblingUnaffected(t *testing.T) {
	// A barrier inside the first branch must not block the second branch —
	// blocking is per subtree.
	a := Wrap(New(InvalidArgument, "a").WithPublic("a public"), "barrier").WithoutPublic()
	b := New(Internal, "b").WithPublic("b public").WithPublicField("from", "b")
	joined := errors.Join(a, b)

	if got := PublicMessage(joined); got != "b public" {
		t.Errorf("PublicMessage = %q, want the sibling branch's message", got)
	}
	if got := PublicFields(joined); got["from"] != "b" {
		t.Errorf("PublicFields = %v, want the sibling branch's field", got)
	}

	// A barrier above the Join blocks every branch.
	all := Wrap(joined, "boundary").WithoutPublic()
	if msg, ok := LookupPublicMessage(all); ok {
		t.Errorf("LookupPublicMessage = %q, want blocked for all branches", msg)
	}
	if got := PublicFields(all); got != nil {
		t.Errorf("PublicFields = %v, want nil", got)
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
