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

// codeInfo holds the metadata for a single Code.
type codeInfo struct {
	name       string
	httpStatus int
	grpcCode   uint32
}

// codes maps Code to codeInfo.
//
// This table is only ever populated at init (for built-ins) and appended to
// by Register before the server starts; it has no protection against
// concurrent writes. Reads (String/HTTPStatus/GRPCCode) happen from many
// goroutines after startup, which is safe as long as all writes finished
// before that point.
var codes = map[Code]codeInfo{
	OK:                 {"OK", http.StatusOK, 0},
	Canceled:           {"CANCELED", 499, 1},
	Unknown:            {"UNKNOWN", http.StatusInternalServerError, 2},
	InvalidArgument:    {"INVALID_ARGUMENT", http.StatusBadRequest, 3},
	DeadlineExceeded:   {"DEADLINE_EXCEEDED", http.StatusGatewayTimeout, 4},
	NotFound:           {"NOT_FOUND", http.StatusNotFound, 5},
	AlreadyExists:      {"ALREADY_EXISTS", http.StatusConflict, 6},
	PermissionDenied:   {"PERMISSION_DENIED", http.StatusForbidden, 7},
	ResourceExhausted:  {"RESOURCE_EXHAUSTED", http.StatusTooManyRequests, 8},
	FailedPrecondition: {"FAILED_PRECONDITION", http.StatusBadRequest, 9},
	Aborted:            {"ABORTED", http.StatusConflict, 10},
	OutOfRange:         {"OUT_OF_RANGE", http.StatusBadRequest, 11},
	Unimplemented:      {"UNIMPLEMENTED", http.StatusNotImplemented, 12},
	Internal:           {"INTERNAL", http.StatusInternalServerError, 13},
	Unavailable:        {"UNAVAILABLE", http.StatusServiceUnavailable, 14},
	DataLoss:           {"DATA_LOSS", http.StatusInternalServerError, 15},
	Unauthenticated:    {"UNAUTHENTICATED", http.StatusUnauthorized, 16},
}

// Register adds a custom code. Call it from an init function, or otherwise
// before the server starts — calling it afterward races with other
// goroutines reading Code.
//
// Panics if c is below customCodeMin (100), if c is already registered, if
// name is empty, if httpStatus is outside [100, 599] (an out-of-range status
// would otherwise panic inside http.ResponseWriter.WriteHeader at request
// time, far from the cause), or if grpcCode is above 16 (gRPC defines codes
// 0–16; anything else is not portable across clients).
func Register(c Code, name string, httpStatus int, grpcCode uint32) {
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
	codes[c] = codeInfo{name: name, httpStatus: httpStatus, grpcCode: grpcCode}
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
