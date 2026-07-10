package grpcerr

import (
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"

	"github.com/repenguin22/errtrail"
)

const rateLimited errtrail.Code = 100

var registerOnce sync.Once

// registerRateLimited registers the shared custom-code fixture exactly once,
// so -count=2 (and multiple tests) don't panic on a duplicate registration.
func registerRateLimited() {
	registerOnce.Do(func() {
		errtrail.Register(rateLimited, "RATE_LIMITED", 429, uint32(codes.ResourceExhausted))
	})
}

// setDomain sets Domain for one test and restores the previous value.
func setDomain(t *testing.T, d string) {
	t.Helper()
	old := Domain
	Domain = d
	t.Cleanup(func() { Domain = old })
}

func TestToStatusCode(t *testing.T) {
	err := errtrail.New(errtrail.NotFound, "internal").WithPublic("User not found")
	st := ToStatus(err)
	if st.Code() != codes.NotFound {
		t.Errorf("Code = %v, want NotFound", st.Code())
	}
	if st.Message() != "User not found" {
		t.Errorf("Message = %q", st.Message())
	}
}

func TestToStatusFallbackMessage(t *testing.T) {
	// public not set -> the message falls back to the code name, never to
	// the internal message and never to HTTP wording.
	err := errtrail.New(errtrail.Internal, "secret")
	st := ToStatus(err)
	if st.Code() != codes.Internal {
		t.Errorf("Code = %v", st.Code())
	}
	if st.Message() != "INTERNAL" {
		t.Errorf("Message = %q, want INTERNAL (code-name fallback)", st.Message())
	}
}

func TestToStatusFallbackNeverEmpty(t *testing.T) {
	// Canceled maps to HTTP 499, which http.StatusText doesn't know — the
	// old PublicMessage-based fallback produced an empty message here.
	if got := ToStatus(errtrail.New(errtrail.Canceled, "ctx canceled")).Message(); got != "CANCELED" {
		t.Errorf("Message = %q, want CANCELED", got)
	}

	// Custom codes get their name even without Domain/ErrorInfo.
	registerRateLimited()
	if got := ToStatus(errtrail.New(rateLimited, "slow down")).Message(); got != "RATE_LIMITED" {
		t.Errorf("Message = %q, want RATE_LIMITED", got)
	}
}

func TestToStatusExplicitPublicUnchanged(t *testing.T) {
	err := errtrail.New(errtrail.Internal, "secret").WithPublic("Something went wrong")
	if got := ToStatus(err).Message(); got != "Something went wrong" {
		t.Errorf("Message = %q, want the explicit public message", got)
	}
}

func TestToStatusNil(t *testing.T) {
	st := ToStatus(nil)
	if st.Code() != codes.OK {
		t.Errorf("Code = %v, want OK", st.Code())
	}
}

func TestToErrorNil(t *testing.T) {
	if ToError(nil) != nil {
		t.Error("ToError(nil) should be nil")
	}
}

func TestToErrorRoundTrip(t *testing.T) {
	err := errtrail.New(errtrail.PermissionDenied, "x").WithPublic("denied")
	gerr := ToError(err)
	st, ok := status.FromError(gerr)
	if !ok {
		t.Fatal("FromError failed")
	}
	if st.Code() != codes.PermissionDenied {
		t.Errorf("Code = %v", st.Code())
	}
	if st.Message() != "denied" {
		t.Errorf("Message = %q", st.Message())
	}
}

func TestCustomCodeMapping(t *testing.T) {
	registerRateLimited()

	err := errtrail.New(rateLimited, "slow down").WithPublic("Too many requests")
	st := ToStatus(err)
	if st.Code() != codes.ResourceExhausted {
		t.Errorf("Code = %v, want ResourceExhausted", st.Code())
	}
}

func TestWrapChainDelegatesCode(t *testing.T) {
	inner := errtrail.New(errtrail.Unavailable, "down")
	outer := errtrail.Wrap(inner, "calling backend")
	if ToStatus(outer).Code() != codes.Unavailable {
		t.Errorf("Code = %v, want Unavailable", ToStatus(outer).Code())
	}
}

// errorInfoOf extracts the single ErrorInfo detail from st, failing the test
// if it is absent or the details look otherwise unexpected.
func errorInfoOf(t *testing.T, st *status.Status) *errdetails.ErrorInfo {
	t.Helper()
	details := st.Details()
	if len(details) != 1 {
		t.Fatalf("len(Details) = %d, want 1", len(details))
	}
	info, ok := details[0].(*errdetails.ErrorInfo)
	if !ok {
		t.Fatalf("details[0] = %T, want *errdetails.ErrorInfo", details[0])
	}
	return info
}

func TestErrorInfoAttachedWithDomain(t *testing.T) {
	setDomain(t, "errtrail.test")

	err := errtrail.New(errtrail.NotFound, "row missing").WithPublic("User not found")
	// Round-trip through ToError / status.FromError, as a gRPC handler would.
	st, ok := status.FromError(ToError(err))
	if !ok {
		t.Fatal("FromError failed")
	}
	info := errorInfoOf(t, st)
	if info.GetReason() != "NOT_FOUND" {
		t.Errorf("Reason = %q, want NOT_FOUND", info.GetReason())
	}
	if info.GetDomain() != "errtrail.test" {
		t.Errorf("Domain = %q, want errtrail.test", info.GetDomain())
	}
}

func TestErrorInfoCarriesCustomCodeName(t *testing.T) {
	registerRateLimited()
	setDomain(t, "errtrail.test")

	// On the wire the numeric code is ResourceExhausted; only the ErrorInfo
	// carries the custom name.
	err := errtrail.New(rateLimited, "slow down")
	st := ToStatus(err)
	if st.Code() != codes.ResourceExhausted {
		t.Errorf("Code = %v, want ResourceExhausted", st.Code())
	}
	if got := errorInfoOf(t, st).GetReason(); got != "RATE_LIMITED" {
		t.Errorf("Reason = %q, want RATE_LIMITED", got)
	}
}

func TestErrorInfoResolvesWrappedCode(t *testing.T) {
	setDomain(t, "errtrail.test")

	inner := errtrail.New(errtrail.Unavailable, "down")
	outer := errtrail.Wrap(inner, "calling backend")
	if got := errorInfoOf(t, ToStatus(outer)).GetReason(); got != "UNAVAILABLE" {
		t.Errorf("Reason = %q, want UNAVAILABLE", got)
	}
}

func TestNoDetailsWithoutDomain(t *testing.T) {
	// Domain defaults to "" — the wire format must stay exactly as before.
	err := errtrail.New(errtrail.NotFound, "x")
	if n := len(ToStatus(err).Details()); n != 0 {
		t.Errorf("len(Details) = %d, want 0 when Domain is unset", n)
	}
}

func TestNoErrorInfoForUnregisteredCode(t *testing.T) {
	setDomain(t, "errtrail.test")

	// An unregistered code's Reason would be "CODE(9999)" — spec-violating
	// and unrecoverable — so no ErrorInfo is attached at all.
	st := ToStatus(errtrail.New(errtrail.Code(9999), "x"))
	if n := len(st.Details()); n != 0 {
		t.Errorf("len(Details) = %d, want 0 for an unregistered code", n)
	}
	// The numeric mapping and the message fallback are unchanged.
	if st.Code() != codes.Unknown {
		t.Errorf("Code = %v, want Unknown", st.Code())
	}
	if st.Message() != "CODE(9999)" {
		t.Errorf("Message = %q, want CODE(9999)", st.Message())
	}
}

func TestNilErrWithDomain(t *testing.T) {
	setDomain(t, "errtrail.test")

	st := ToStatus(nil)
	if st.Code() != codes.OK {
		t.Errorf("Code = %v, want OK", st.Code())
	}
	if n := len(st.Details()); n != 0 {
		t.Errorf("len(Details) = %d, want 0 on OK", n)
	}
}

func TestFromErrorRoundTripBuiltin(t *testing.T) {
	orig := errtrail.New(errtrail.NotFound, "row missing").WithPublic("User not found")
	got := FromError(ToError(orig))
	if code := errtrail.CodeOf(got); code != errtrail.NotFound {
		t.Errorf("CodeOf = %v, want NotFound", code)
	}
}

func TestFromErrorRecoversCustomCode(t *testing.T) {
	registerRateLimited()
	setDomain(t, "errtrail.test")

	// On the wire: numeric code ResourceExhausted + ErrorInfo{Reason: RATE_LIMITED}.
	gerr := ToError(errtrail.New(rateLimited, "slow down"))
	got := FromError(gerr)
	if code := errtrail.CodeOf(got); code != rateLimited {
		t.Errorf("CodeOf = %v, want rateLimited (recovered from Reason)", code)
	}
}

func TestFromErrorCustomCodeWithoutDetails(t *testing.T) {
	registerRateLimited()
	// Domain unset: no ErrorInfo on the wire, so only the numeric code
	// survives and the conversion degrades to ResourceExhausted.
	gerr := ToError(errtrail.New(rateLimited, "slow down"))
	if code := errtrail.CodeOf(FromError(gerr)); code != errtrail.ResourceExhausted {
		t.Errorf("CodeOf = %v, want ResourceExhausted", code)
	}
}

func TestTrustedDomainMatch(t *testing.T) {
	registerRateLimited()
	setDomain(t, "errtrail.test")

	gerr := ToError(errtrail.New(rateLimited, "slow down"))
	got := FromError(gerr, TrustedDomain("errtrail.test"))
	if code := errtrail.CodeOf(got); code != rateLimited {
		t.Errorf("CodeOf = %v, want rateLimited (trusted domain matches)", code)
	}
}

func TestTrustedDomainMismatch(t *testing.T) {
	registerRateLimited()
	setDomain(t, "errtrail.test")

	gerr := ToError(errtrail.New(rateLimited, "slow down"))
	got := FromError(gerr, TrustedDomain("other.example.com"))
	if code := errtrail.CodeOf(got); code != errtrail.ResourceExhausted {
		t.Errorf("CodeOf = %v, want ResourceExhausted (untrusted domain degrades to the wire code)", code)
	}
}

func TestTrustedDomainMultipleAndAppend(t *testing.T) {
	registerRateLimited()
	setDomain(t, "errtrail.test")
	gerr := ToError(errtrail.New(rateLimited, "slow down"))

	// Any of several domains matches.
	got := FromError(gerr, TrustedDomain("a.example.com", "errtrail.test"))
	if code := errtrail.CodeOf(got); code != rateLimited {
		t.Errorf("CodeOf = %v, want rateLimited (one of several domains)", code)
	}
	// Passing the option twice appends rather than replaces.
	got = FromError(gerr, TrustedDomain("a.example.com"), TrustedDomain("errtrail.test"))
	if code := errtrail.CodeOf(got); code != rateLimited {
		t.Errorf("CodeOf = %v, want rateLimited (options accumulate)", code)
	}
}

func TestTrustedDomainZeroArgsNoOp(t *testing.T) {
	registerRateLimited()
	setDomain(t, "errtrail.test")

	gerr := ToError(errtrail.New(rateLimited, "slow down"))
	if code := errtrail.CodeOf(FromError(gerr, TrustedDomain())); code != rateLimited {
		t.Errorf("CodeOf = %v, want rateLimited (no domains = no restriction)", code)
	}
}

func TestTrustedDomainBlocksForeignTaxonomy(t *testing.T) {
	registerRateLimited()
	// A foreign service that reuses BOTH the Reason name and the numeric
	// code slips through the default double check — that combination is
	// exactly what TrustedDomain exists to reject.
	st, err := status.New(codes.ResourceExhausted, "slow down").WithDetails(&errdetails.ErrorInfo{
		Reason: "RATE_LIMITED",
		Domain: "foreign.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := errtrail.CodeOf(FromStatus(st)); code != rateLimited {
		t.Errorf("CodeOf (default) = %v, want rateLimited (documents the default)", code)
	}
	if code := errtrail.CodeOf(FromStatus(st, TrustedDomain("errtrail.test"))); code != errtrail.ResourceExhausted {
		t.Errorf("CodeOf (trusted) = %v, want ResourceExhausted (foreign domain rejected)", code)
	}
}

func TestFromStatusRejectsMismatchedReason(t *testing.T) {
	registerRateLimited()
	// A foreign taxonomy might reuse the name RATE_LIMITED under a different
	// numeric code. The registered gRPC code (ResourceExhausted) does not
	// match the wire code (Internal), so recovery must not trigger.
	st, err := status.New(codes.Internal, "boom").WithDetails(&errdetails.ErrorInfo{
		Reason: "RATE_LIMITED",
		Domain: "foreign.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := errtrail.CodeOf(FromStatus(st)); code != errtrail.Internal {
		t.Errorf("CodeOf = %v, want Internal (mismatched Reason must be ignored)", code)
	}
}

func TestFromStatusUnknownReason(t *testing.T) {
	st, err := status.New(codes.NotFound, "gone").WithDetails(&errdetails.ErrorInfo{
		Reason: "NO_SUCH_LOCAL_NAME",
		Domain: "foreign.example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if code := errtrail.CodeOf(FromStatus(st)); code != errtrail.NotFound {
		t.Errorf("CodeOf = %v, want NotFound (numeric mapping)", code)
	}
}

func TestFromStatusCodeAbove16(t *testing.T) {
	st := status.New(codes.Code(99), "nonstandard")
	if code := errtrail.CodeOf(FromStatus(st)); code != errtrail.Unknown {
		t.Errorf("CodeOf = %v, want Unknown", code)
	}
}

func TestFromErrorNilSafety(t *testing.T) {
	if FromError(nil) != nil {
		t.Error("FromError(nil) should be nil")
	}
	if FromStatus(nil) != nil {
		t.Error("FromStatus(nil) should be nil")
	}
	if FromStatus(status.New(codes.OK, "")) != nil {
		t.Error("FromStatus(OK) should be nil")
	}
}

func TestFromErrorPreservesCause(t *testing.T) {
	orig := errors.New("plain failure")
	got := FromError(orig)
	if code := errtrail.CodeOf(got); code != errtrail.Unknown {
		t.Errorf("CodeOf = %v, want Unknown for a non-status error", code)
	}
	if !errors.Is(got, orig) {
		t.Error("errors.Is should see the original error through the conversion")
	}
	if got.Error() != "plain failure" {
		t.Errorf("Error() = %q, want the original message", got.Error())
	}
}

func TestWithDetailsFallbackWhenRejected(t *testing.T) {
	// The WithDetails-failure fallback is kept as defense against a proto
	// marshal failure: the status itself must never be lost over details.
	// Register no longer accepts a code mapped to gRPC OK, so trigger the
	// rejection directly with an OK status (WithDetails always rejects OK).
	st := withDetails(status.New(codes.OK, ""), []protoadapt.MessageV1{
		&errdetails.ErrorInfo{Reason: "NOT_FOUND", Domain: "errtrail.test"},
	})
	if st.Code() != codes.OK {
		t.Errorf("Code = %v, want OK (status preserved)", st.Code())
	}
	if n := len(st.Details()); n != 0 {
		t.Errorf("len(Details) = %d, want 0 (attach must fail cleanly)", n)
	}
}

const throttled errtrail.Code = 150

var registerThrottledOnce sync.Once

// registerThrottled registers the RetryAfter fixture exactly once.
func registerThrottled() {
	registerThrottledOnce.Do(func() {
		errtrail.Register(throttled, "THROTTLED", 429, uint32(codes.ResourceExhausted),
			errtrail.RetryAfter(2*time.Second))
	})
}

func TestRetryInfoAttachedForRetryAfterCode(t *testing.T) {
	registerThrottled()
	// Domain deliberately unset: RetryInfo is independent of the ErrorInfo
	// opt-in — registering a delay is the opt-in.
	st := ToStatus(errtrail.New(throttled, "bucket empty"))
	details := st.Details()
	if len(details) != 1 {
		t.Fatalf("len(Details) = %d, want 1 (RetryInfo only)", len(details))
	}
	info, ok := details[0].(*errdetails.RetryInfo)
	if !ok {
		t.Fatalf("details[0] = %T, want *errdetails.RetryInfo", details[0])
	}
	if got := info.GetRetryDelay().AsDuration(); got != 2*time.Second {
		t.Errorf("RetryDelay = %v, want 2s", got)
	}
}

func TestNoRetryInfoWithoutDelay(t *testing.T) {
	registerRateLimited() // retryable-capable custom code, but no RetryAfter
	if n := len(ToStatus(errtrail.New(rateLimited, "x")).Details()); n != 0 {
		t.Errorf("len(Details) = %d, want 0 without a registered delay", n)
	}
	// Built-ins are retryable but cannot carry a delay.
	if n := len(ToStatus(errtrail.New(errtrail.Unavailable, "x")).Details()); n != 0 {
		t.Errorf("len(Details) = %d, want 0 for a built-in", n)
	}
}

func TestBadRequestFromViolations(t *testing.T) {
	err := errtrail.New(errtrail.InvalidArgument, "bad request").
		WithFieldViolation("email", "must be a valid email address").
		WithFieldViolation("age", "must be at least 0")
	details := ToStatus(err).Details()
	if len(details) != 1 {
		t.Fatalf("len(Details) = %d, want 1 (BadRequest only)", len(details))
	}
	br, ok := details[0].(*errdetails.BadRequest)
	if !ok {
		t.Fatalf("details[0] = %T, want *errdetails.BadRequest", details[0])
	}
	vs := br.GetFieldViolations()
	if len(vs) != 2 {
		t.Fatalf("len(FieldViolations) = %d, want 2", len(vs))
	}
	if vs[0].GetField() != "email" || vs[0].GetDescription() != "must be a valid email address" {
		t.Errorf("violations[0] = %v", vs[0])
	}
	if vs[1].GetField() != "age" {
		t.Errorf("violations[1] = %v", vs[1])
	}
}

func TestAllDetailsTogetherInOrder(t *testing.T) {
	registerThrottled()
	setDomain(t, "errtrail.test")

	err := errtrail.New(throttled, "bucket empty").
		WithFieldViolation("query", "too broad")
	details := ToStatus(err).Details()
	if len(details) != 3 {
		t.Fatalf("len(Details) = %d, want 3", len(details))
	}
	if _, ok := details[0].(*errdetails.ErrorInfo); !ok {
		t.Errorf("details[0] = %T, want ErrorInfo", details[0])
	}
	if _, ok := details[1].(*errdetails.RetryInfo); !ok {
		t.Errorf("details[1] = %T, want RetryInfo", details[1])
	}
	if _, ok := details[2].(*errdetails.BadRequest); !ok {
		t.Errorf("details[2] = %T, want BadRequest", details[2])
	}
}

func TestRetryDelayHelper(t *testing.T) {
	registerThrottled()

	gerr := ToError(errtrail.New(throttled, "bucket empty"))
	if d, ok := RetryDelay(gerr); !ok || d != 2*time.Second {
		t.Errorf("RetryDelay = %v, %v; want 2s, true", d, ok)
	}

	if _, ok := RetryDelay(nil); ok {
		t.Error("RetryDelay(nil) ok = true, want false")
	}
	if _, ok := RetryDelay(errors.New("plain")); ok {
		t.Error("RetryDelay(plain) ok = true, want false")
	}
	if _, ok := RetryDelay(ToError(errtrail.New(errtrail.Unavailable, "x"))); ok {
		t.Error("RetryDelay(no detail) ok = true, want false")
	}
}
