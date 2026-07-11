// Package otelerr bridges errtrail errors and OpenTelemetry spans at the
// request boundary. It depends on go.opentelemetry.io/otel, so it's kept in
// a module separate from the errtrail core — users who don't use OTel never
// pull it in.
//
// Spans are treated as an internal channel, like logs: Record puts the
// internal message chain (err.Error()), the code name, and the With attrs
// on the span, and deliberately leaves the public message out (it's for
// response generation). Span attributes are exported to your own tracing
// backend and never propagate on the wire — only the trace context IDs do.
//
// "errtrail.code" is a reserved attribute key carrying the taxonomy code
// name. A With attr sharing that key is dropped rather than exported
// alongside it — letting both through would leave a tracing backend to
// arbitrate a duplicate key (first-wins, last-wins, or an array, depending
// on the backend), silently corrupting the one attribute alerts and
// dashboards key off of.
//
// The span status follows the OpenTelemetry gRPC semantic conventions for
// server spans: only server-fault codes (Unknown, DeadlineExceeded,
// Unimplemented, Internal, Unavailable, DataLoss) set the status to Error;
// client-fault codes such as NotFound or InvalidArgument leave it unset, so
// lookup misses don't light the trace UI up as an error storm. The
// exception event is recorded either way.
package otelerr

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/repenguin22/errtrail"
)

// codeKey is the span-event attribute carrying the errtrail code name.
const codeKey = attribute.Key("errtrail.code")

// Record records err on the span in ctx: an exception event carrying the
// errtrail code name and attrs, plus — for server-fault codes only (see the
// package doc) — a span status of Error with err.Error() as description.
// No-op when err is nil or ctx carries no recording span.
func Record(ctx context.Context, err error) {
	RecordSpan(trace.SpanFromContext(ctx), err)
}

// RecordSpan is Record for a span you already hold. No-op when err or span
// is nil, or the span is not recording.
func RecordSpan(span trace.Span, err error) {
	if err == nil || span == nil || !span.IsRecording() {
		return
	}
	code := errtrail.CodeOf(err)
	attrs := []attribute.KeyValue{codeKey.String(code.String())}
	for _, a := range errtrail.Attrs(err) {
		if a.Key == string(codeKey) {
			// Reserved: a user attr sharing this key must not duplicate or
			// shadow the taxonomy attribute exporters and backends see.
			continue
		}
		attrs = append(attrs, otelAttr(a))
	}
	span.RecordError(err, trace.WithAttributes(attrs...))
	if serverFault(code) {
		span.SetStatus(codes.Error, err.Error())
	}
}

// TraceAttrs returns slog attrs (trace_id, span_id) for the span in ctx,
// for attaching to an error via errtrail's With so logs can be joined with
// traces. Returns nil when ctx carries no valid span context.
func TraceAttrs(ctx context.Context) []slog.Attr {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return nil
	}
	return []slog.Attr{
		slog.String("trace_id", sc.TraceID().String()),
		slog.String("span_id", sc.SpanID().String()),
	}
}

// serverFault reports whether the code marks a server-side failure, per the
// OpenTelemetry gRPC semantic conventions for server spans. Custom codes
// map through their registered gRPC code, so the registry stays the single
// source of truth.
//
// Deriving from GRPCCode() rather than HTTPStatus() is not a bias toward
// gRPC services: for the built-ins the six server-fault gRPC codes are
// precisely the six codes mapped to HTTP 5xx, so HTTP services get the same
// classification. The two can diverge only for a custom code registered
// with an inconsistent pair (e.g. a 5xx HTTP status under a client-fault
// gRPC code) — register consistently and no HTTP-specific variant of Record
// is needed.
func serverFault(c errtrail.Code) bool {
	switch c.GRPCCode() {
	case 2, 4, 12, 13, 14, 15: // UNKNOWN, DEADLINE_EXCEEDED, UNIMPLEMENTED, INTERNAL, UNAVAILABLE, DATA_LOSS
		return true
	}
	return false
}

// otelAttr converts one slog.Attr into an OTel attribute. Primitive kinds
// map to their native attribute type; everything else (Duration, Time,
// Group, Any) degrades to the slog string form. LogValuer values are
// resolved first.
func otelAttr(a slog.Attr) attribute.KeyValue {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return attribute.String(a.Key, v.String())
	case slog.KindInt64:
		return attribute.Int64(a.Key, v.Int64())
	case slog.KindFloat64:
		return attribute.Float64(a.Key, v.Float64())
	case slog.KindBool:
		return attribute.Bool(a.Key, v.Bool())
	default:
		// Uint64 may overflow Int64; Duration/Time/Group/Any have no native
		// attribute type — the slog string form is lossless enough here.
		return attribute.String(a.Key, v.String())
	}
}
