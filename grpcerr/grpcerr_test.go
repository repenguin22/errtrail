package grpcerr

import (
	"errors"
	"sync"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

const legacyOK errtrail.Code = 101

var registerLegacyOKOnce sync.Once

func TestDetailsFallbackOnOKMappedCode(t *testing.T) {
	// Register allows mapping a custom code to gRPC OK; WithDetails rejects
	// OK statuses, so ToStatus must fall back to the undetailed status
	// rather than lose it.
	registerLegacyOKOnce.Do(func() {
		errtrail.Register(legacyOK, "LEGACY_OK", 200, uint32(codes.OK))
	})
	setDomain(t, "errtrail.test")

	st := ToStatus(errtrail.New(legacyOK, "odd mapping"))
	if st.Code() != codes.OK {
		t.Errorf("Code = %v, want OK", st.Code())
	}
	if n := len(st.Details()); n != 0 {
		t.Errorf("len(Details) = %d, want 0 (details cannot attach to OK)", n)
	}
}
