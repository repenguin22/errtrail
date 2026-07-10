package errtrail

import (
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
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

// registry is an immutable snapshot of the code tables: the Code metadata
// and the name -> Code reverse index for CodeByName. Readers load the
// current snapshot through registryPtr; Register replaces the whole
// snapshot (copy-on-write), so a lookup never observes a partial write.
// Never mutate a snapshot that has been stored.
type registry struct {
	codes map[Code]codeInfo
	names map[string]Code
}

// clone returns a mutable deep copy for a writer to modify and then Store.
func (r *registry) clone() *registry {
	next := &registry{
		codes: make(map[Code]codeInfo, len(r.codes)+1),
		names: make(map[string]Code, len(r.names)+1),
	}
	for c, info := range r.codes {
		next.codes[c] = info
	}
	for n, c := range r.names {
		next.names[n] = c
	}
	return next
}

var registryPtr atomic.Pointer[registry]

// registerMu serializes writers only (Register and the test helpers); two
// concurrent clone-and-swaps would silently drop the loser's entry.
// Readers never take it — they do one atomic pointer load.
var registerMu sync.Mutex

func init() {
	reg := &registry{codes: builtins, names: make(map[string]Code, len(builtins))}
	for c, info := range builtins {
		reg.names[info.name] = c
	}
	registryPtr.Store(reg)
}

// CodeByName returns the Code registered under name — built-ins included,
// e.g. "NOT_FOUND". Reports false for names it does not know. Useful for
// parsing code names from configuration or from a wire format (see
// grpcerr's ErrorInfo.Reason recovery).
func CodeByName(name string) (Code, bool) {
	c, ok := registryPtr.Load().names[name]
	return c, ok
}

// codeInfo holds the metadata for a single Code.
type codeInfo struct {
	name       string
	httpStatus int
	grpcCode   uint32
	retryable  bool
}

// builtins seeds the first registry snapshot at init. It is never read
// after that — every lookup goes through registryPtr — and must not be
// mutated, since the initial snapshot references it directly.
//
// Retryable built-ins: DeadlineExceeded (may succeed under a fresh
// deadline), ResourceExhausted (after backoff), Aborted (transaction-style
// retry), Unavailable (the canonical transient failure). Canceled is not —
// the caller gave up deliberately — and Unknown is conservatively not.
var builtins = map[Code]codeInfo{
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

// Register adds a custom code. It is safe to call at any time, including
// concurrently with lookups — the registry is replaced atomically
// (copy-on-write), so readers never observe a partial write. Registering
// from an init function is still the recommended pattern: every request
// then sees the same taxonomy, and CodeByName / gRPC Reason recovery on
// other services assume a stable registry.
//
// Panics if c is below customCodeMin (100), if c or name is already
// registered (names are the reverse-lookup key for CodeByName, so they must
// be unique), if name does not match [A-Z][A-Z0-9_]+[A-Z0-9] or exceeds 63
// characters (the errdetails.ErrorInfo.Reason constraints — grpcerr puts the
// name on the wire as the Reason), if httpStatus is outside [400, 599] (a
// Code classifies an error; mapping it to a 2xx/3xx would make problem.Write
// emit a success response), or if grpcCode is outside [1, 16] (gRPC defines
// codes 0–16, and 0 is OK — an error mapped to OK would make grpcerr.ToError
// return nil, silently dropping the error).
func Register(c Code, name string, httpStatus int, grpcCode uint32, opts ...RegisterOption) {
	if c < customCodeMin {
		panic("errtrail: custom code must be >= 100, got " + strconv.FormatUint(uint64(c), 10))
	}
	if !validCodeName(name) {
		panic("errtrail: code name must match [A-Z][A-Z0-9_]+[A-Z0-9] and be at most 63 characters, got " + strconv.Quote(name))
	}
	if httpStatus < 400 || httpStatus > 599 {
		panic("errtrail: httpStatus must be in [400, 599], got " + strconv.Itoa(httpStatus))
	}
	if grpcCode == 0 || grpcCode > 16 {
		panic("errtrail: grpcCode must be in [1, 16] (0 is OK, which would drop the error), got " + strconv.FormatUint(uint64(grpcCode), 10))
	}

	registerMu.Lock()
	defer registerMu.Unlock()
	reg := registryPtr.Load()
	if _, ok := reg.codes[c]; ok {
		panic("errtrail: code already registered: " + strconv.FormatUint(uint64(c), 10))
	}
	if _, ok := reg.names[name]; ok {
		panic("errtrail: code name already registered: " + name)
	}
	info := codeInfo{name: name, httpStatus: httpStatus, grpcCode: grpcCode}
	for _, o := range opts {
		o(&info)
	}
	next := reg.clone()
	next.codes[c] = info
	next.names[name] = c
	registryPtr.Store(next)
}

// validCodeName reports whether name matches [A-Z][A-Z0-9_]+[A-Z0-9] with at
// most 63 characters — the errdetails.ErrorInfo.Reason constraints, checked
// here so a registered name is always legal on the gRPC wire. Hand-rolled to
// keep the core free of a regexp dependency.
func validCodeName(name string) bool {
	if len(name) < 3 || len(name) > 63 {
		return false
	}
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		case c == '_':
			if i == 0 || i == len(name)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// String returns the code's name, e.g. "NOT_FOUND" for built-ins or
// "CODE(123)" for an unregistered custom code.
func (c Code) String() string {
	if info, ok := registryPtr.Load().codes[c]; ok {
		return info.name
	}
	return "CODE(" + strconv.FormatUint(uint64(c), 10) + ")"
}

// HTTPStatus returns the corresponding HTTP status code. Unregistered codes
// return 500.
func (c Code) HTTPStatus() int {
	if info, ok := registryPtr.Load().codes[c]; ok {
		return info.httpStatus
	}
	return http.StatusInternalServerError
}

// GRPCCode returns the corresponding gRPC code as a number. 0–16 map to
// themselves, custom codes return the value passed to Register, and
// unregistered codes return 2 (UNKNOWN). Returned as uint32 so this package
// need not depend on the grpc package.
func (c Code) GRPCCode() uint32 {
	if info, ok := registryPtr.Load().codes[c]; ok {
		return info.grpcCode
	}
	return 2 // UNKNOWN
}

// Retryable reports whether the code classifies a failure worth retrying.
// Built-ins: DeadlineExceeded, ResourceExhausted, Aborted, and Unavailable
// return true; everything else returns false. Custom codes return true only
// if registered with the Retryable option. Unregistered codes return false.
func (c Code) Retryable() bool {
	if info, ok := registryPtr.Load().codes[c]; ok {
		return info.retryable
	}
	return false
}
