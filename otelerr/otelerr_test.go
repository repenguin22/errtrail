package otelerr

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/repenguin22/errtrail"
)

// Custom-code fixtures, registered once so -count=2 doesn't panic.
const (
	flakyDep   errtrail.Code = 130 // maps to gRPC UNAVAILABLE (server fault)
	notInCache errtrail.Code = 131 // maps to gRPC NOT_FOUND (client fault)
)

var registerOnce sync.Once

func registerFixtures() {
	registerOnce.Do(func() {
		errtrail.Register(flakyDep, "FLAKY_DEP", 503, 14)
		errtrail.Register(notInCache, "NOT_IN_CACHE", 404, 5)
	})
}

// startSpan returns a recording span and the recorder that captures it.
func startSpan(t *testing.T) (context.Context, trace.Span, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	return ctx, span, sr
}

// ended ends the span and returns its exported form.
func ended(t *testing.T, sr *tracetest.SpanRecorder, span trace.Span) sdktrace.ReadOnlySpan {
	t.Helper()
	span.End()
	spans := sr.Ended()
	if len(spans) != 1 {
		t.Fatalf("len(Ended()) = %d, want 1", len(spans))
	}
	return spans[0]
}

// eventAttr returns the value of key on the single exception event.
func eventAttr(t *testing.T, ro sdktrace.ReadOnlySpan, key attribute.Key) (attribute.Value, bool) {
	t.Helper()
	events := ro.Events()
	if len(events) != 1 {
		t.Fatalf("len(Events()) = %d, want 1", len(events))
	}
	if events[0].Name != "exception" {
		t.Fatalf("event name = %q, want exception", events[0].Name)
	}
	for _, kv := range events[0].Attributes {
		if kv.Key == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func TestRecordServerFault(t *testing.T) {
	ctx, span, sr := startSpan(t)

	err := errtrail.New(errtrail.Internal, "db write failed").
		WithPublic("Something went wrong").
		With(slog.String("table", "users"), slog.Int("attempt", 3),
			slog.Bool("retried", true), slog.Duration("elapsed", 250*time.Millisecond))
	Record(ctx, err)

	ro := ended(t, sr, span)
	if ro.Status().Code != codes.Error {
		t.Errorf("status = %v, want Error", ro.Status().Code)
	}
	if ro.Status().Description != "db write failed" {
		t.Errorf("description = %q, want the internal message", ro.Status().Description)
	}

	if v, ok := eventAttr(t, ro, codeKey); !ok || v.AsString() != "INTERNAL" {
		t.Errorf("errtrail.code = %v (present=%v), want INTERNAL", v.String(), ok)
	}
	if v, ok := eventAttr(t, ro, "table"); !ok || v.AsString() != "users" {
		t.Errorf("table attr = %v (present=%v)", v.String(), ok)
	}
	if v, ok := eventAttr(t, ro, "attempt"); !ok || v.AsInt64() != 3 {
		t.Errorf("attempt attr = %v (present=%v), want int64 3", v.String(), ok)
	}
	if v, ok := eventAttr(t, ro, "retried"); !ok || !v.AsBool() {
		t.Errorf("retried attr = %v (present=%v), want true", v.String(), ok)
	}
	if v, ok := eventAttr(t, ro, "elapsed"); !ok || v.AsString() != "250ms" {
		t.Errorf("elapsed attr = %v (present=%v), want string 250ms", v.String(), ok)
	}
	// The public message must not appear anywhere on the event.
	if _, ok := eventAttr(t, ro, "public"); ok {
		t.Error("public message leaked onto the span event")
	}
}

func TestRecordDropsUserAttrReservingCodeKey(t *testing.T) {
	// A With attr sharing the reserved "errtrail.code" key must not
	// duplicate the taxonomy attribute on the exported event (Round 7
	// review finding): a tracing backend could apply first-wins,
	// last-wins, or array semantics, any of which corrupts the one
	// attribute alerts and dashboards key off of.
	ctx, span, sr := startSpan(t)
	err := errtrail.New(errtrail.Internal, "x").With(slog.String("errtrail.code", "SPOOFED"))
	Record(ctx, err)

	ro := ended(t, sr, span)
	events := ro.Events()
	if len(events) != 1 {
		t.Fatalf("len(Events()) = %d, want 1", len(events))
	}
	var matches int
	var last attribute.Value
	for _, kv := range events[0].Attributes {
		if kv.Key == codeKey {
			matches++
			last = kv.Value
		}
	}
	if matches != 1 {
		t.Fatalf("errtrail.code appears %d times, want exactly 1", matches)
	}
	if last.AsString() != "INTERNAL" {
		t.Errorf("errtrail.code = %q, want the real code INTERNAL, not the spoofed attr", last.AsString())
	}
}

func TestRecordClientFaultLeavesStatusUnset(t *testing.T) {
	ctx, span, sr := startSpan(t)

	Record(ctx, errtrail.New(errtrail.NotFound, "row missing"))

	ro := ended(t, sr, span)
	if ro.Status().Code != codes.Unset {
		t.Errorf("status = %v, want Unset for a client-fault code", ro.Status().Code)
	}
	// The exception event is still recorded.
	if v, ok := eventAttr(t, ro, codeKey); !ok || v.AsString() != "NOT_FOUND" {
		t.Errorf("errtrail.code = %v (present=%v), want NOT_FOUND", v.String(), ok)
	}
}

func TestRecordCustomCodesMapThroughGRPCCode(t *testing.T) {
	registerFixtures()

	ctx, span, sr := startSpan(t)
	Record(ctx, errtrail.New(flakyDep, "upstream flapped"))
	if got := ended(t, sr, span).Status().Code; got != codes.Error {
		t.Errorf("FLAKY_DEP (grpc UNAVAILABLE) status = %v, want Error", got)
	}

	ctx2, span2, sr2 := startSpan(t)
	Record(ctx2, errtrail.New(notInCache, "cold cache"))
	if got := ended(t, sr2, span2).Status().Code; got != codes.Unset {
		t.Errorf("NOT_IN_CACHE (grpc NOT_FOUND) status = %v, want Unset", got)
	}
}

func TestRecordPlainErrorIsServerFault(t *testing.T) {
	ctx, span, sr := startSpan(t)
	Record(ctx, errors.New("unclassified"))
	ro := ended(t, sr, span)
	if ro.Status().Code != codes.Error {
		t.Errorf("status = %v, want Error (Unknown is a server fault)", ro.Status().Code)
	}
	if v, ok := eventAttr(t, ro, codeKey); !ok || v.AsString() != "UNKNOWN" {
		t.Errorf("errtrail.code = %v (present=%v), want UNKNOWN", v.String(), ok)
	}
}

func TestRecordNoOps(t *testing.T) {
	// nil error: nothing recorded.
	ctx, span, sr := startSpan(t)
	Record(ctx, nil)
	if got := len(ended(t, sr, span).Events()); got != 0 {
		t.Errorf("nil err recorded %d events, want 0", got)
	}

	// Context without a span, and a nil span: must not panic.
	Record(context.Background(), errtrail.New(errtrail.Internal, "x"))
	RecordSpan(nil, errtrail.New(errtrail.Internal, "x"))
}

func TestTraceAttrs(t *testing.T) {
	if got := TraceAttrs(context.Background()); got != nil {
		t.Errorf("TraceAttrs(no span) = %v, want nil", got)
	}

	ctx, span, _ := startSpan(t)
	defer span.End()
	attrs := TraceAttrs(ctx)
	if len(attrs) != 2 {
		t.Fatalf("len(attrs) = %d, want 2", len(attrs))
	}
	sc := span.SpanContext()
	if attrs[0].Key != "trace_id" || attrs[0].Value.String() != sc.TraceID().String() {
		t.Errorf("trace_id attr = %v", attrs[0])
	}
	if attrs[1].Key != "span_id" || attrs[1].Value.String() != sc.SpanID().String() {
		t.Errorf("span_id attr = %v", attrs[1])
	}
}
