package errtrail

import "log/slog"

// LogValue implements slog.LogValuer. It represents the error as a group
// value carrying the internal message, code, trace, and any attached attrs
// into structured logs. The public message, the public fields
// (WithPublicField), the field violations (WithFieldViolation), and the
// retry delay (WithRetryDelay) are deliberately left out — they're meant
// for response generation, not internal logs.
//
// The keys "msg", "code", and "trace" are used by the group itself. An attr
// attached via With under one of those keys is emitted alongside as a
// duplicate (slog does not deduplicate keys) — treat them as reserved.
func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.Value{}
	}

	// Gather code, trace, and attrs in one pass instead of three walks.
	c := collect(e)
	traceStrs := make([]string, len(c.trace))
	for i, f := range c.trace {
		traceStrs[i] = f.String()
	}

	group := []slog.Attr{
		slog.String("msg", e.Error()),
		slog.String("code", c.code.String()),
		slog.Any("trace", traceStrs),
	}
	group = append(group, c.attrs...)

	return slog.GroupValue(group...)
}
