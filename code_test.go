package errtrail

import (
	"net/http"
	"sync"
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
	t.Cleanup(func() { unregister(rateLimited, "RATE_LIMITED") })

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
	t.Cleanup(func() { unregister(flaky, "FLAKY_UPSTREAM") })

	if !flaky.Retryable() {
		t.Error("Retryable() = false, want true when registered with Retryable()")
	}
}

func TestRegisterPanicsOnDuplicateName(t *testing.T) {
	const first Code = 105
	Register(first, "DUP_NAME", 500, 2)
	t.Cleanup(func() { unregister(first, "DUP_NAME") })

	defer func() {
		if recover() == nil {
			t.Error("expected panic for duplicate name")
		}
	}()
	Register(Code(106), "DUP_NAME", 503, 14) // different code, same name
}

// TestRegisterConcurrentWithReaders is the regression test for the
// copy-on-write registry: readers loop over every lookup while codes are
// registered and unregistered. Under -race this fails deterministically
// against a plain-map implementation.
func TestRegisterConcurrentWithReaders(t *testing.T) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = Unavailable.String()
				_ = Code(120).HTTPStatus()
				_, _ = CodeByName("CONCURRENT_CODE")
				_ = IsRetryable(New(Code(120), "x"))
			}
		}()
	}

	const churn Code = 120
	for range 200 {
		Register(churn, "CONCURRENT_CODE", 503, 14, Retryable())
		unregister(churn, "CONCURRENT_CODE")
	}
	close(stop)
	wg.Wait()
}

func TestRegisterAfterReadsIsVisible(t *testing.T) {
	const late Code = 121
	// Force reads before registering.
	if got := late.String(); got != "CODE(121)" {
		t.Fatalf("pre-register String() = %q", got)
	}

	Register(late, "LATE_CODE", 503, 14)
	t.Cleanup(func() { unregister(late, "LATE_CODE") })

	if got := late.String(); got != "LATE_CODE" {
		t.Errorf("post-register String() = %q, want LATE_CODE", got)
	}
	if c, ok := CodeByName("LATE_CODE"); !ok || c != late {
		t.Errorf("CodeByName(LATE_CODE) = %v, %v", c, ok)
	}
}

func TestCodeByName(t *testing.T) {
	if c, ok := CodeByName("NOT_FOUND"); !ok || c != NotFound {
		t.Errorf("CodeByName(NOT_FOUND) = %v, %v", c, ok)
	}

	const named Code = 107
	Register(named, "NAMED_CODE", 500, 2)
	t.Cleanup(func() { unregister(named, "NAMED_CODE") })
	if c, ok := CodeByName("NAMED_CODE"); !ok || c != named {
		t.Errorf("CodeByName(NAMED_CODE) = %v, %v", c, ok)
	}

	if _, ok := CodeByName("NO_SUCH_NAME"); ok {
		t.Error("CodeByName(NO_SUCH_NAME) should report false")
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

// unregister removes a code from the registry — test cleanup only. Like
// Register, it clones the current snapshot and swaps it in atomically.
func unregister(c Code, name string) {
	registerMu.Lock()
	defer registerMu.Unlock()
	next := registryPtr.Load().clone()
	delete(next.codes, c)
	delete(next.names, name)
	registryPtr.Store(next)
}

func TestRegisterPanicsOnDuplicate(t *testing.T) {
	const dup Code = 101
	Register(dup, "DUP", 500, 2)
	t.Cleanup(func() { unregister(dup, "DUP") })

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
