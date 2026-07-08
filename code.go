package errtrail

import (
	"net/http"
	"strconv"
)

// Code classifies an error. Values 0–16 share the same meaning and numeric
// value as gRPC's codes.Code. HTTP and gRPC statuses are derived from Code
// via a lookup table.
type Code uint32

// Built-in codes. 0–16 match gRPC's codes.Code.
const (
	OK                 Code = 0 // No error. Not expected to be set on an Error.
	Canceled           Code = 1
	Unknown            Code = 2
	InvalidArgument    Code = 3
	DeadlineExceeded   Code = 4
	NotFound           Code = 5
	AlreadyExists      Code = 6
	PermissionDenied   Code = 7
	ResourceExhausted  Code = 8
	FailedPrecondition Code = 9
	Aborted            Code = 10
	OutOfRange         Code = 11
	Unimplemented      Code = 12
	Internal           Code = 13
	Unavailable        Code = 14
	DataLoss           Code = 15
	Unauthenticated    Code = 16
)

// customCodeMin is the lower bound for custom codes. 0–99 are reserved for
// built-ins.
const customCodeMin Code = 100

// codeNames is the name -> Code reverse index, seeded from codes at init
// and kept in sync by Register (which rejects duplicate names). Same
// concurrency contract as codes: writes only before the server starts.
var codeNames = map[string]Code{}

func init() {
	for c, info := range codes {
		codeNames[info.name] = c
	}
}

// CodeByName returns the Code registered under name — built-ins included,
// e.g. "NOT_FOUND". Reports false for names it does not know. Useful for
// parsing code names from configuration or from a wire format (see
// grpcerr's ErrorInfo.Reason recovery).
func CodeByName(name string) (Code, bool) {
	c, ok := codeNames[name]
	return c, ok
}

// codeInfo holds the metadata for a single Code.
type codeInfo struct {
	name       string
	httpStatus int
	grpcCode   uint32
	retryable  bool
}

// codes maps Code to codeInfo.
//
// This table is only ever populated at init (for built-ins) and appended to
// by Register before the server starts; it has no protection against
// concurrent writes. Reads (String/HTTPStatus/GRPCCode) happen from many
// goroutines after startup, which is safe as long as all writes finished
// before that point.
// Retryable built-ins: DeadlineExceeded (may succeed under a fresh
// deadline), ResourceExhausted (after backoff), Aborted (transaction-style
// retry), Unavailable (the canonical transient failure). Canceled is not —
// the caller gave up deliberately — and Unknown is conservatively not.
var codes = map[Code]codeInfo{
	OK:                 {"OK", http.StatusOK, 0, false},
	Canceled:           {"CANCELED", 499, 1, false},
	Unknown:            {"UNKNOWN", http.StatusInternalServerError, 2, false},
	InvalidArgument:    {"INVALID_ARGUMENT", http.StatusBadRequest, 3, false},
	DeadlineExceeded:   {"DEADLINE_EXCEEDED", http.StatusGatewayTimeout, 4, true},
	NotFound:           {"NOT_FOUND", http.StatusNotFound, 5, false},
	AlreadyExists:      {"ALREADY_EXISTS", http.StatusConflict, 6, false},
	PermissionDenied:   {"PERMISSION_DENIED", http.StatusForbidden, 7, false},
	ResourceExhausted:  {"RESOURCE_EXHAUSTED", http.StatusTooManyRequests, 8, true},
	FailedPrecondition: {"FAILED_PRECONDITION", http.StatusBadRequest, 9, false},
	Aborted:            {"ABORTED", http.StatusConflict, 10, true},
	OutOfRange:         {"OUT_OF_RANGE", http.StatusBadRequest, 11, false},
	Unimplemented:      {"UNIMPLEMENTED", http.StatusNotImplemented, 12, false},
	Internal:           {"INTERNAL", http.StatusInternalServerError, 13, false},
	Unavailable:        {"UNAVAILABLE", http.StatusServiceUnavailable, 14, true},
	DataLoss:           {"DATA_LOSS", http.StatusInternalServerError, 15, false},
	Unauthenticated:    {"UNAUTHENTICATED", http.StatusUnauthorized, 16, false},
}

// RegisterOption customizes a code being registered.
type RegisterOption func(*codeInfo)

// Retryable returns a RegisterOption that marks the code being registered
// as retryable, so IsRetryable and (Code).Retryable report true for it.
// Codes are not retryable by default.
func Retryable() RegisterOption {
	return func(info *codeInfo) { info.retryable = true }
}

// Register adds a custom code. Call it from an init function, or otherwise
// before the server starts — calling it afterward races with other
// goroutines reading Code.
//
// Panics if c is below customCodeMin (100), if c or name is already
// registered (names are the reverse-lookup key for CodeByName, so they must
// be unique), if name is empty, if httpStatus is outside [100, 599] (an
// out-of-range status would otherwise panic inside
// http.ResponseWriter.WriteHeader at request time, far from the cause), or
// if grpcCode is above 16 (gRPC defines codes 0–16; anything else is not
// portable across clients).
func Register(c Code, name string, httpStatus int, grpcCode uint32, opts ...RegisterOption) {
	if c < customCodeMin {
		panic("errtrail: custom code must be >= 100, got " + strconv.FormatUint(uint64(c), 10))
	}
	if name == "" {
		panic("errtrail: code name must not be empty")
	}
	if httpStatus < 100 || httpStatus > 599 {
		panic("errtrail: httpStatus must be in [100, 599], got " + strconv.Itoa(httpStatus))
	}
	if grpcCode > 16 {
		panic("errtrail: grpcCode must be a gRPC code (0-16), got " + strconv.FormatUint(uint64(grpcCode), 10))
	}
	if _, ok := codes[c]; ok {
		panic("errtrail: code already registered: " + strconv.FormatUint(uint64(c), 10))
	}
	if _, ok := codeNames[name]; ok {
		panic("errtrail: code name already registered: " + name)
	}
	info := codeInfo{name: name, httpStatus: httpStatus, grpcCode: grpcCode}
	for _, o := range opts {
		o(&info)
	}
	codes[c] = info
	codeNames[name] = c
}

// String returns the code's name, e.g. "NOT_FOUND" for built-ins or
// "CODE(123)" for an unregistered custom code.
func (c Code) String() string {
	if info, ok := codes[c]; ok {
		return info.name
	}
	return "CODE(" + strconv.FormatUint(uint64(c), 10) + ")"
}

// HTTPStatus returns the corresponding HTTP status code. Unregistered codes
// return 500.
func (c Code) HTTPStatus() int {
	if info, ok := codes[c]; ok {
		return info.httpStatus
	}
	return http.StatusInternalServerError
}

// GRPCCode returns the corresponding gRPC code as a number. 0–16 map to
// themselves, custom codes return the value passed to Register, and
// unregistered codes return 2 (UNKNOWN). Returned as uint32 so this package
// need not depend on the grpc package.
func (c Code) GRPCCode() uint32 {
	if info, ok := codes[c]; ok {
		return info.grpcCode
	}
	return 2 // UNKNOWN
}

// Retryable reports whether the code classifies a failure worth retrying.
// Built-ins: DeadlineExceeded, ResourceExhausted, Aborted, and Unavailable
// return true; everything else returns false. Custom codes return true only
// if registered with the Retryable option. Unregistered codes return false.
func (c Code) Retryable() bool {
	if info, ok := codes[c]; ok {
		return info.retryable
	}
	return false
}
