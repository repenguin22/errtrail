package errtrail

import (
	"fmt"
	"log/slog"
	"slices"
)

// Error is errtrail's core error type. It is immutable — there is no API to
// mutate a field after construction. The With* methods return shallow
// copies, so a single *Error can be safely shared and derived from across
// goroutines.
type Error struct {
	code   Code          // Zero value OK means "unset"; CodeOf delegates to the inner Error.
	msg    string        // Internal message (for logs). Never shown to a client.
	public string        // Public message shown to clients. Empty means unset.
	cause  error         // The wrapped error. May be nil.
	pc     uintptr       // One recorded caller frame (resolved lazily). 0 means "none".
	attrs  []slog.Attr   // Structured-logging attributes (internal, logs only).
	fields []publicField // Public extension fields (client-visible).

	// noPublicBelow marks this node as a public-data barrier (WithoutPublic):
	// the cause chain below it contributes no public message and no public
	// fields. This node's own public data — and anything added by an outer
	// wrap — still applies; internal msg, attrs, and trace are unaffected.
	noPublicBelow bool
}

// publicField is one client-visible key-value pair attached via
// WithPublicField. Kept separate from attrs, which are internal-only.
type publicField struct {
	key   string
	value any
}

// New creates a new error, recording one caller frame.
func New(code Code, msg string) *Error {
	return &Error{code: code, msg: msg, pc: caller()}
}

// Newf is the fmt.Sprintf form of New. %w is not supported here (use Wrap to
// wrap an error).
func Newf(code Code, format string, args ...any) *Error {
	return &Error{code: code, msg: fmt.Sprintf(format, args...), pc: caller()}
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
	return &Error{msg: msg, cause: err, pc: caller()}
}

// Wrapf is the fmt.Sprintf form of Wrap. Returns nil when err is nil (see
// Wrap for the typed-nil caveat).
func Wrapf(err error, format string, args ...any) *Error {
	if err == nil {
		return nil
	}
	return &Error{msg: fmt.Sprintf(format, args...), cause: err, pc: caller()}
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
// chain below this node contributes no public message and no public fields to
// LookupPublicMessage, PublicMessage, PublicFields, or anything built on them
// (problem responses, gRPC status messages). This node's own public data —
// and anything added by an outer wrap — still applies, and the internal
// message, attrs, and trace are unaffected. Does not record a new frame.
//
// Use it when reclassifying an error whose original public data must not
// reach the client through the new response. For example, converting a
// NotFound that carries a public "User not found" into a PermissionDenied
// (to hide whether the resource exists) without WithoutPublic would leak
// that message — and any public fields — through the 403:
//
//	return errtrail.Wrap(err, "reclassify lookup").
//	    WithCode(errtrail.PermissionDenied).
//	    WithoutPublic().
//	    WithPublic("Forbidden") // optional: the new public message, set above the barrier
//
// The barrier blocks only the chain below the node, so chaining WithPublic /
// WithPublicField before or after WithoutPublic on the same node makes no
// difference — both operate on the node itself.
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
	// Clip before appending so the copy never shares a backing array with e.attrs.
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
	// Clip before appending so the copy never shares a backing array with e.fields.
	cp.fields = append(slices.Clip(e.fields), publicField{key: key, value: value})
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
