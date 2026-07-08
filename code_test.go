package errtrail

import (
	"net/http"
	"testing"
)

func TestBuiltinCodeMapping(t *testing.T) {
	cases := []struct {
		code  Code
		name  string
		http  int
		grpc  uint32
		retry bool
	}{
		{OK, "OK", 200, 0, false},
		{Canceled, "CANCELED", 499, 1, false},
		{Unknown, "UNKNOWN", 500, 2, false},
		{InvalidArgument, "INVALID_ARGUMENT", 400, 3, false},
		{DeadlineExceeded, "DEADLINE_EXCEEDED", 504, 4, true},
		{NotFound, "NOT_FOUND", 404, 5, false},
		{AlreadyExists, "ALREADY_EXISTS", 409, 6, false},
		{PermissionDenied, "PERMISSION_DENIED", 403, 7, false},
		{ResourceExhausted, "RESOURCE_EXHAUSTED", 429, 8, true},
		{FailedPrecondition, "FAILED_PRECONDITION", 400, 9, false},
		{Aborted, "ABORTED", 409, 10, true},
		{OutOfRange, "OUT_OF_RANGE", 400, 11, false},
		{Unimplemented, "UNIMPLEMENTED", 501, 12, false},
		{Internal, "INTERNAL", 500, 13, false},
		{Unavailable, "UNAVAILABLE", 503, 14, true},
		{DataLoss, "DATA_LOSS", 500, 15, false},
		{Unauthenticated, "UNAUTHENTICATED", 401, 16, false},
	}
	for _, c := range cases {
		if got := c.code.String(); got != c.name {
			t.Errorf("Code(%d).String() = %q, want %q", c.code, got, c.name)
		}
		if got := c.code.HTTPStatus(); got != c.http {
			t.Errorf("Code(%d).HTTPStatus() = %d, want %d", c.code, got, c.http)
		}
		if got := c.code.GRPCCode(); got != c.grpc {
			t.Errorf("Code(%d).GRPCCode() = %d, want %d", c.code, got, c.grpc)
		}
		if got := c.code.Retryable(); got != c.retry {
			t.Errorf("Code(%d).Retryable() = %v, want %v", c.code, got, c.retry)
		}
	}
}

func TestUnregisteredCode(t *testing.T) {
	c := Code(9999)
	if got := c.String(); got != "CODE(9999)" {
		t.Errorf("String() = %q, want CODE(9999)", got)
	}
	if got := c.HTTPStatus(); got != http.StatusInternalServerError {
		t.Errorf("HTTPStatus() = %d, want 500", got)
	}
	if got := c.GRPCCode(); got != 2 {
		t.Errorf("GRPCCode() = %d, want 2 (UNKNOWN)", got)
	}
	if c.Retryable() {
		t.Error("Retryable() = true, want false for unregistered code")
	}
}

func TestRegister(t *testing.T) {
	const rateLimited Code = 100
	Register(rateLimited, "RATE_LIMITED", http.StatusTooManyRequests, 8)
	t.Cleanup(func() { delete(codes, rateLimited) })

	if got := rateLimited.String(); got != "RATE_LIMITED" {
		t.Errorf("String() = %q", got)
	}
	if got := rateLimited.HTTPStatus(); got != 429 {
		t.Errorf("HTTPStatus() = %d", got)
	}
	if got := rateLimited.GRPCCode(); got != 8 {
		t.Errorf("GRPCCode() = %d", got)
	}
	// Registered without the Retryable option — not retryable by default.
	if rateLimited.Retryable() {
		t.Error("Retryable() = true, want false without the option")
	}
}

func TestRegisterRetryableOption(t *testing.T) {
	const flaky Code = 103
	Register(flaky, "FLAKY_UPSTREAM", http.StatusServiceUnavailable, 14, Retryable())
	t.Cleanup(func() { delete(codes, flaky) })

	if !flaky.Retryable() {
		t.Error("Retryable() = false, want true when registered with Retryable()")
	}
}

func TestRegisterPanicsBelowMin(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for code < 100")
		}
	}()
	Register(Code(50), "BAD", 500, 2)
}

func TestRegisterPanicsOnDuplicate(t *testing.T) {
	const dup Code = 101
	Register(dup, "DUP", 500, 2)
	t.Cleanup(func() { delete(codes, dup) })

	defer func() {
		if recover() == nil {
			t.Error("expected panic for duplicate registration")
		}
	}()
	Register(dup, "DUP2", 500, 2)
}

func TestRegisterValidatesArguments(t *testing.T) {
	cases := []struct {
		desc       string
		name       string
		httpStatus int
		grpcCode   uint32
	}{
		{"empty name", "", 500, 2},
		{"httpStatus below 100", "BAD", 0, 2},
		{"httpStatus above 599", "BAD", 600, 2},
		{"grpcCode above 16", "BAD", 500, 17},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for %s", c.desc)
				}
			}()
			Register(Code(102), c.name, c.httpStatus, c.grpcCode)
		})
	}
}
