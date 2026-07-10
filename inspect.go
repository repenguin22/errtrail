package errtrail

import (
	"log/slog"
	"net/http"
)

// walk visits every *Error in err's chain depth-first: itself, then
// Unwrap(). For a branching chain like errors.Join (Unwrap() []error), the
// first branch is visited first. Stops as soon as fn returns false.
//
// fn's blocked argument reports whether the node sits below a WithoutPublic
// barrier: its public message and fields must not be exposed. Only the
// public-data collectors consult it — code, trace, and attrs ignore it.
// Blocking is per subtree, so a barrier in one Join branch never blocks a
// sibling branch (which is also why a blocked walker must keep walking
// rather than stop: a later branch may still contribute).
//
// Cyclic chains are not detected — as with the standard errors package,
// that's the caller's responsibility to avoid.
func walk(err error, fn func(e *Error, blocked bool) bool) {
	walkFrom(err, false, fn)
}

// walkFrom is walk with an inherited blocked state, threaded through Join
// recursion so each branch keeps its own copy (a variable shared across
// branches would leak a barrier from one branch into its siblings). The
// bool result exists for the recursion itself: false propagates "fn asked
// to stop" out of nested branches.
func walkFrom(err error, blocked bool, fn func(*Error, bool) bool) bool {
	for err != nil {
		// Guard against a typed-nil *Error stored in a non-nil error
		// interface (the classic Go footgun): fn dereferences the *Error, so
		// a nil one would panic. Skipping it is safe — its Unwrap returns nil,
		// which ends the walk below.
		if e, ok := err.(*Error); ok && e != nil {
			if !fn(e, blocked) {
				return false
			}
			// The barrier takes effect below the node: the node's own public
			// data still counts (WithoutPublic().WithPublic(...) works), the
			// chain underneath is blocked.
			if e.noPublicBelow {
				blocked = true
			}
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if !walkFrom(sub, blocked, fn) {
					return false
				}
			}
			return true
		default:
			return true
		}
	}
	return true
}

// CodeOf walks err's chain from the outside in and returns the Code of the
// first *Error whose code != OK. Returns OK if err is nil, and Unknown if no
// *Error is found (or every *Error in the chain has an unset code).
func CodeOf(err error) Code {
	if err == nil {
		return OK
	}
	found := Unknown
	walk(err, func(e *Error, _ bool) bool {
		if e.code != OK {
			found = e.code
			return false
		}
		return true
	})
	return found
}

// IsRetryable reports whether err classifies a failure worth retrying,
// derived purely from CodeOf(err) — see (Code).Retryable for the built-in
// set. Returns false for nil and for non-errtrail errors (Unknown is
// conservatively not retryable). No errors.Is inspection (e.g. of
// context.Canceled) is performed.
//
// It is a transience hint, nothing more: it says the failure class tends to
// clear up on its own, not that replaying the request is safe. Idempotency,
// retry budgets, and honoring server pushback remain the caller's
// responsibility.
func IsRetryable(err error) bool {
	return CodeOf(err).Retryable()
}

// LookupPublicMessage walks err's chain from the outside in and returns the
// first explicitly-set public message, reporting whether one was found. It
// never falls back to anything — use it when the caller wants its own
// fallback policy (grpcerr falls back to the code name; an i18n layer might
// pick a translation). PublicMessage is this plus a two-level fallback
// (http.StatusText, then the code name). Public messages below a
// WithoutPublic barrier are not considered.
func LookupPublicMessage(err error) (string, bool) {
	msg := ""
	walk(err, func(e *Error, blocked bool) bool {
		if !blocked && e.public != "" {
			msg = e.public
			return false
		}
		return true
	})
	return msg, msg != ""
}

// PublicMessage walks err's chain from the outside in and returns the first
// non-empty public message found. If none is set, it falls back to
// http.StatusText(CodeOf(err).HTTPStatus()); when http.StatusText does not
// know the status either — notably Canceled (499) and custom codes mapped to
// a non-standard HTTP status — it falls back to the code name (e.g.
// "CANCELED"), the same rule the problem title and the gRPC status message
// use. It thus never returns "" for a non-nil err (PublicMessage(nil) is
// still ""), and never falls back to an internal message. Use
// LookupPublicMessage to apply a different fallback.
func PublicMessage(err error) string {
	if err == nil {
		return ""
	}
	if msg, ok := LookupPublicMessage(err); ok {
		return msg
	}
	code := CodeOf(err)
	if s := http.StatusText(code.HTTPStatus()); s != "" {
		return s
	}
	return code.String()
}

// PublicFields walks err's chain from the outside in and collects the
// public fields attached via WithPublicField into a map. For a duplicate
// key, the outermost *Error's value wins (consistent with CodeOf and
// PublicMessage: layers closer to the boundary override); within one
// *Error, the last WithPublicField wins (consistent with calling WithPublic
// twice). Fields below a WithoutPublic barrier are not collected. Returns
// nil if no fields are found. Never includes attrs or internal messages.
func PublicFields(err error) map[string]any {
	var m map[string]any
	walk(err, func(e *Error, blocked bool) bool {
		if blocked {
			return true
		}
		// Reverse order, so the node's last-added field is seen first and
		// wins the first-seen map guard below (last-write-wins within a
		// node); nodes further out were visited earlier and still override.
		for i := len(e.fields) - 1; i >= 0; i-- {
			f := e.fields[i]
			if m == nil {
				m = make(map[string]any)
			}
			if _, ok := m[f.key]; !ok {
				m[f.key] = f.value
			}
		}
		return true
	})
	return m
}

// Trace returns the frames of every *Error in err's chain, ordered from the
// outermost (where it was last wrapped) to the innermost (where it
// originated). Returns nil if no *Error is found.
func Trace(err error) []Frame {
	var frames []Frame
	walk(err, func(e *Error, _ bool) bool {
		frames = append(frames, resolveFrame(e.pc, e.msg))
		return true
	})
	return frames
}

// Attrs concatenates the attrs of every *Error in err's chain, from
// outermost to innermost. Duplicate keys are not deduplicated (left to
// slog's own behavior). Returns nil if no *Error is found.
func Attrs(err error) []slog.Attr {
	var attrs []slog.Attr
	walk(err, func(e *Error, _ bool) bool {
		attrs = append(attrs, e.attrs...)
		return true
	})
	return attrs
}

// collected holds everything the %+v formatter and the slog LogValuer need,
// gathered in a single pass instead of one walk per field.
type collected struct {
	code   Code          // resolved code, or Unknown if none is set
	public string        // first explicitly-set public message, "" if none
	trace  []Frame       // every frame, outermost to innermost
	attrs  []slog.Attr   // every attr, outermost to innermost
	fields []publicField // every public field, outermost to innermost (duplicates kept)
}

// collect walks err's chain once, gathering the resolved code, the first
// public message, the full trace, all attrs, and all public fields. The code
// and public results match CodeOf and the "no fallback" public lookup —
// including the WithoutPublic barrier, so the %+v / LogValue public lines
// show what a client can actually see; trace and attrs match Trace and Attrs
// (never blocked). fields keeps duplicates in walk order — display-only,
// unlike PublicFields' outermost-wins map. It exists so the formatter and
// logger don't walk the chain several times over.
func collect(err error) collected {
	c := collected{code: Unknown}
	haveCode := false
	walk(err, func(e *Error, blocked bool) bool {
		if !haveCode && e.code != OK {
			c.code = e.code
			haveCode = true
		}
		if !blocked {
			if c.public == "" && e.public != "" {
				c.public = e.public
			}
			c.fields = append(c.fields, e.fields...)
		}
		c.trace = append(c.trace, resolveFrame(e.pc, e.msg))
		c.attrs = append(c.attrs, e.attrs...)
		return true
	})
	return c
}
