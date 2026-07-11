package errtrail

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"
)

// Error is errtrail's core error type. It is immutable — there is no API to
// mutate a field after construction. The With* methods return shallow
// copies, so a single *Error can be safely shared and derived from across
// goroutines.
type Error struct {
	code       Code             // Zero value OK means "unset"; CodeOf delegates to the inner Error.
	msg        string           // Internal message (for logs). Never shown to a client.
	public     string           // Public message shown to clients. Empty means unset.
	cause      error            // The wrapped error. May be nil.
	pc         uintptr          // One recorded caller frame (resolved lazily). 0 means "none".
	attrs      []slog.Attr      // Structured-logging attributes (internal, logs only).
	fields     []publicField    // Public extension fields (client-visible).
	violations []FieldViolation // Field-level validation violations (client-visible).
	retryDelay time.Duration    // Per-error retry delay (client-visible). 0 means "unset".

	// noPublicBelow marks this node as a public-data barrier (WithoutPublic):
	// the cause chain below it contributes no public message, no public
	// fields, and no field violations. This node's own public data — and
	// anything added by an outer wrap — still applies; internal msg, attrs,
	// and trace are unaffected.
	noPublicBelow bool
}

// publicField is one client-visible key-value pair attached via
// WithPublicField. Kept separate from attrs, which are internal-only.
type publicField struct {
	key   string
	value any
}

// FieldViolation is one client-visible, field-level validation violation
// attached via WithFieldViolation and collected by FieldViolations. The
// json tags give it the {"field", "description"} shape when problem.From
// emits violations as the "errors" extension member; grpcerr.ToStatus maps
// it onto errdetails.BadRequest_FieldViolation.
type FieldViolation struct {
	Field       string `json:"field"`       // request field, e.g. "email" or "profile.age"
	Description string `json:"description"` // why it was rejected, safe to show a client
}

// New creates a new error, recording one caller frame.
func New(code Code, msg string) *Error {
	return &Error{code: code, msg: msg, pc: caller(0)}
}

// Newf is the fmt.Sprintf form of New. %w is not supported here (use Wrap to
// wrap an error).
func Newf(code Code, format string, args ...any) *Error {
	return &Error{code: code, msg: fmt.Sprintf(format, args...), pc: caller(0)}
}

// Wrap wraps err, recording one caller frame. The code is left unset (OK),
// so CodeOf delegates to the Code further down the chain. To change the
// code, use Wrap(...).WithCode(c).
//
// Wrap returns nil when err is nil, which keeps chained builder calls safe
// (all builder methods are nil-receiver safe). Caution: the return type is
// *Error, so returning that nil directly from a function declared to return
// error yields a non-nil error holding a typed nil. Keep the usual guard in
// such functions:
//
//	if err != nil {
//	    return errtrail.Wrap(err, "load profile")
//	}
func Wrap(err error, msg string) *Error {
	if err == nil {
		return nil
	}
	return &Error{msg: msg, cause: err, pc: caller(0)}
}

// Wrapf is the fmt.Sprintf form of Wrap. Returns nil when err is nil (see
// Wrap for the typed-nil caveat).
func Wrapf(err error, format string, args ...any) *Error {
	if err == nil {
		return nil
	}
	return &Error{msg: fmt.Sprintf(format, args...), cause: err, pc: caller(0)}
}

// NewSkip is New for error factories: it skips `skip` additional stack
// frames when recording the caller frame, so a helper that constructs
// errors on behalf of its own caller can point the frame at that caller
// instead of at itself.
//
//	func notFound(msg string) *errtrail.Error {
//	    return errtrail.NewSkip(1, errtrail.NotFound, msg)
//	}
//
// skip = 0 is exactly New; skip = 1 records the factory's caller; each
// further wrapper layer adds one. A skip past the top of the stack records
// the "unknown" frame. Panics if skip is negative.
func NewSkip(skip int, code Code, msg string) *Error {
	if skip < 0 {
		panic("errtrail: NewSkip requires a non-negative skip, got " + strconv.Itoa(skip))
	}
	return &Error{code: code, msg: msg, pc: caller(skip)}
}

// WrapSkip is Wrap for error factories (see NewSkip for the skip contract).
// Returns nil when err is nil, with Wrap's typed-nil caveat. Panics if skip
// is negative.
func WrapSkip(skip int, err error, msg string) *Error {
	if skip < 0 {
		panic("errtrail: WrapSkip requires a non-negative skip, got " + strconv.Itoa(skip))
	}
	if err == nil {
		return nil
	}
	return &Error{msg: msg, cause: err, pc: caller(skip)}
}

// WithCode returns a copy with the code replaced. Does not record a new frame.
func (e *Error) WithCode(c Code) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.code = c
	return &cp
}

// WithPublic returns a copy with the public message set. Does not record a
// new frame.
func (e *Error) WithPublic(msg string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.public = msg
	return &cp
}

// WithoutPublic returns a copy that acts as a public-data barrier: the cause
// chain below this node contributes no public message, no public fields, no
// field violations, and no retry delay to LookupPublicMessage,
// PublicMessage, PublicFields, FieldViolations, LookupRetryDelay, or
// anything built on them (problem responses, gRPC status messages and
// details). This node's own public data — and anything added by an outer
// wrap — still applies, and the internal message, attrs, and trace are
// unaffected. A delay registered on the Code with errtrail.RetryAfter is
// registry configuration, not error data, and is not blocked. Does not
// record a new frame.
//
// Use it when reclassifying an error whose original public data must not
// reach the client through the new response. For example, converting a
// NotFound that carries a public "User not found" into a PermissionDenied
// (to hide whether the resource exists) without WithoutPublic would leak
// that message — and any public fields or field violations — through the
// 403:
//
//	return errtrail.Wrap(err, "reclassify lookup").
//	    WithCode(errtrail.PermissionDenied).
//	    WithoutPublic().
//	    WithPublic("Forbidden") // optional: the new public message, set above the barrier
//
// The barrier blocks only the chain below the node, so chaining WithPublic /
// WithPublicField / WithFieldViolation / WithRetryDelay before or after
// WithoutPublic on the same node makes no difference — all operate on the
// node itself.
//
// For the same reason, call WithoutPublic on a fresh Wrap of the error — as
// in the example above — NOT on the error value itself: err.WithoutPublic()
// leaves err's own public message, fields, violations, and retry delay
// above the barrier, still visible to clients.
func (e *Error) WithoutPublic() *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.noPublicBelow = true
	return &cp
}

// With returns a copy with the given slog.Attr values appended. Does not
// record a new frame.
//
// Attr values are stored by reference — no deep copy is taken. Hand over an
// immutable snapshot: mutating a slice or map you passed later changes what
// LogValue and %+v emit, and doing so concurrently with logging is a data
// race.
//
// Example: e.With(slog.String("user_id", id), slog.Int("attempt", n))
func (e *Error) With(attrs ...slog.Attr) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	// Clip so the copy never shares appendable capacity with e.attrs — a
	// later append on either side always reallocates instead of clobbering
	// the other. (With no attrs to add, the same read-only array is kept.)
	cp.attrs = append(slices.Clip(e.attrs), attrs...)
	return &cp
}

// WithPublicField returns a copy with a public key-value field appended.
// Does not record a new frame.
//
// Unlike With (whose attrs go to logs only), public fields are
// client-visible: problem.From emits them as RFC 9457 extension members.
// Never put internal data in a public field. Like the public message, they
// are excluded from LogValue.
//
// The value is stored by reference — no deep copy is taken. Hand over an
// immutable snapshot: mutating a slice or map you passed later changes what
// problem.Write emits, and doing so concurrently with a request is a data
// race.
//
// Example: e.WithPublicField("errors", violations)
func (e *Error) WithPublicField(key string, value any) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	// Clip so the copy never shares appendable capacity with e.fields (see With).
	cp.fields = append(slices.Clip(e.fields), publicField{key: key, value: value})
	return &cp
}

// WithFieldViolation returns a copy with a field-level validation violation
// appended. Does not record a new frame.
//
// Violations are client-visible, like the public message and public fields:
// problem.From emits them as the "errors" extension member and
// grpcerr.ToStatus attaches them as an errdetails.BadRequest. Never put
// internal data in one. They are excluded from LogValue and blocked below a
// WithoutPublic barrier.
//
// Example:
//
//	errtrail.New(errtrail.InvalidArgument, "validation failed").
//	    WithPublic("Validation failed").
//	    WithFieldViolation("email", "must be a valid email address").
//	    WithFieldViolation("age", "must be at least 0")
func (e *Error) WithFieldViolation(field, description string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	// Clip so the copy never shares appendable capacity with e.violations
	// (see With).
	cp.violations = append(slices.Clip(e.violations), FieldViolation{Field: field, Description: description})
	return &cp
}

// WithRetryDelay returns a copy carrying a per-error retry delay — the
// fourth client-visible channel. Use it for dynamic server pushback, where
// the actual wait is known at request time (a rate limiter's time to the
// next token, the remaining quota window):
//
//	return errtrail.New(throttled, "bucket empty").
//	    WithRetryDelay(limiter.NextAvailable())
//
// grpcerr.ToStatus emits it as the errdetails.RetryInfo detail, taking
// precedence over the static delay registered with errtrail.RetryAfter.
// problem deliberately does not emit it — set the Retry-After header in the
// handler from LookupRetryDelay if you want it over HTTP.
//
// A non-positive d carries no recommendation ("retry after zero" is not a
// hint) and returns the receiver unchanged. Setting a delay does NOT make
// the code retryable — IsRetryable stays derived from the Code alone;
// register the code with errtrail.Retryable or errtrail.RetryAfter if
// clients should classify it as transient. Does not record a new frame.
func (e *Error) WithRetryDelay(d time.Duration) *Error {
	if e == nil {
		return nil
	}
	if d <= 0 {
		return e
	}
	cp := *e
	cp.retryDelay = d
	return &cp
}

// Error returns "msg: cause", or just msg if cause is nil. If msg is empty
// and cause is set, it returns cause.Error() alone (no stray colon).
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch {
	case e.cause == nil:
		return e.msg
	case e.msg == "":
		return e.cause.Error()
	default:
		return e.msg + ": " + e.cause.Error()
	}
}

// Unwrap returns the wrapped error, letting the standard errors.Is /
// errors.As walk the chain.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
