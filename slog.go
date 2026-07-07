package errtrail

import "log/slog"

// LogValue implements slog.LogValuer. It represents the error as a group
// value carrying the internal message, code, trace, and any attached attrs
// into structured logs. The public message is deliberately left out — it's
// meant for response generation, not internal logs.
func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.Value{}
	}

	trace := Trace(e)
	traceStrs := make([]string, len(trace))
	for i, f := range trace {
		traceStrs[i] = f.String()
	}

	group := []slog.Attr{
		slog.String("msg", e.Error()),
		slog.String("code", CodeOf(e).String()),
		slog.Any("trace", traceStrs),
	}
	group = append(group, Attrs(e)...)

	return slog.GroupValue(group...)
}
