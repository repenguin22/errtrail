package errtrail

import (
	"log/slog"
	"net/http"
)

// walk visits every *Error in err's chain depth-first: itself, then
// Unwrap(). For a branching chain like errors.Join (Unwrap() []error), the
// first branch is visited first. Stops as soon as fn returns false (and
// returns false itself).
//
// Cyclic chains are not detected — as with the standard errors package,
// that's the caller's responsibility to avoid.
func walk(err error, fn func(*Error) bool) bool {
	for err != nil {
		// Guard against a typed-nil *Error stored in a non-nil error
		// interface (the classic Go footgun): fn dereferences the *Error, so
		// a nil one would panic. Skipping it is safe — its Unwrap returns nil,
		// which ends the walk below.
		if e, ok := err.(*Error); ok && e != nil {
			if !fn(e) {
				return false
			}
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if !walk(sub, fn) {
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
	walk(err, func(e *Error) bool {
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
func IsRetryable(err error) bool {
	return CodeOf(err).Retryable()
}

// PublicMessage walks err's chain from the outside in and returns the first
// non-empty public message found. If none is set, it falls back to
// http.StatusText(CodeOf(err).HTTPStatus()). It never falls back to an
// internal message.
//
// http.StatusText returns "" for statuses it does not know — notably Canceled
// (499) and custom codes mapped to a non-standard HTTP status — so for those
// codes with no explicit public message this returns "". Set WithPublic on
// such codes if a client-facing message is required.
func PublicMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := ""
	walk(err, func(e *Error) bool {
		if e.public != "" {
			msg = e.public
			return false
		}
		return true
	})
	if msg != "" {
		return msg
	}
	return http.StatusText(CodeOf(err).HTTPStatus())
}

// PublicFields walks err's chain from the outside in and collects the
// public fields attached via WithPublicField into a map. For a duplicate
// key, the outermost value wins (consistent with CodeOf and PublicMessage:
// layers closer to the boundary override). Returns nil if no fields are
// found. Never includes attrs or internal messages.
func PublicFields(err error) map[string]any {
	var m map[string]any
	walk(err, func(e *Error) bool {
		for _, f := range e.fields {
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
	walk(err, func(e *Error) bool {
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
	walk(err, func(e *Error) bool {
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
// and public results match CodeOf and the "no fallback" public lookup; trace
// and attrs match Trace and Attrs. fields keeps duplicates in walk order —
// display-only, unlike PublicFields' outermost-wins map. It exists so the
// formatter and logger don't walk the chain several times over.
func collect(err error) collected {
	c := collected{code: Unknown}
	haveCode := false
	walk(err, func(e *Error) bool {
		if !haveCode && e.code != OK {
			c.code = e.code
			haveCode = true
		}
		if c.public == "" && e.public != "" {
			c.public = e.public
		}
		c.trace = append(c.trace, resolveFrame(e.pc, e.msg))
		c.attrs = append(c.attrs, e.attrs...)
		c.fields = append(c.fields, e.fields...)
		return true
	})
	return c
}
