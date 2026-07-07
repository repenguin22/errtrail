package errtrail

import (
	"net/http"
	"testing"
)

func TestBuiltinCodeMapping(t *testing.T) {
	cases := []struct {
		code Code
		name string
		http int
		grpc uint32
	}{
		{OK, "OK", 200, 0},
		{Canceled, "CANCELED", 499, 1},
		{Unknown, "UNKNOWN", 500, 2},
		{InvalidArgument, "INVALID_ARGUMENT", 400, 3},
		{DeadlineExceeded, "DEADLINE_EXCEEDED", 504, 4},
		{NotFound, "NOT_FOUND", 404, 5},
		{AlreadyExists, "ALREADY_EXISTS", 409, 6},
		{PermissionDenied, "PERMISSION_DENIED", 403, 7},
		{ResourceExhausted, "RESOURCE_EXHAUSTED", 429, 8},
		{FailedPrecondition, "FAILED_PRECONDITION", 400, 9},
		{Aborted, "ABORTED", 409, 10},
		{OutOfRange, "OUT_OF_RANGE", 400, 11},
		{Unimplemented, "UNIMPLEMENTED", 501, 12},
		{Internal, "INTERNAL", 500, 13},
		{Unavailable, "UNAVAILABLE", 503, 14},
		{DataLoss, "DATA_LOSS", 500, 15},
		{Unauthenticated, "UNAUTHENTICATED", 401, 16},
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
