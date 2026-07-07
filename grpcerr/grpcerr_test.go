package grpcerr

import (
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/repenguin22/errtrail"
)

const rateLimited errtrail.Code = 100

var registerOnce sync.Once

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
	// public not set -> PublicMessage falls back to http.StatusText.
	err := errtrail.New(errtrail.Internal, "secret")
	st := ToStatus(err)
	if st.Code() != codes.Internal {
		t.Errorf("Code = %v", st.Code())
	}
	if st.Message() == "secret" {
		t.Error("leaked internal message")
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
	// Register only once, so -count=2 doesn't panic on a duplicate registration.
	registerOnce.Do(func() {
		errtrail.Register(rateLimited, "RATE_LIMITED", 429, uint32(codes.ResourceExhausted))
	})

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
