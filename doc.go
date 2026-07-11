// Package errtrail is a Go error library for web services (HTTP / gRPC).
//
// Design pillars:
//
//   - Code is the source of truth. HTTP and gRPC statuses are derived from
//     it via a lookup table. Code values 0–16 share the same meaning and
//     numeric value as gRPC's codes.Code.
//   - The core depends on the standard library only. The gRPC conversion
//     lives in a separate grpcerr module.
//   - New and Wrap each record just one caller frame, so the propagation
//     path can be traced. No full stack traces are captured (construction
//     costs roughly 200ns / 144 B / 1 alloc as of v1.3).
//   - Fully compatible with the standard errors package (Is / As / Unwrap /
//     Join).
//   - Internal messages and attrs (for logs) are separated from the four
//     client-visible channels: public messages, public extension fields,
//     field violations, and the retry delay.
//   - Implements slog.LogValuer, so structured logs get code, trace, and
//     attrs for free.
//
// Typical flow:
//
//	// At the source: attach a code and an internal message.
//	if row == nil {
//	    return errtrail.New(errtrail.NotFound, "user row missing").
//	        WithPublic("User not found").
//	        With(slog.String("user_id", id))
//	}
//
//	// In a middle layer: wrap to add context. The code is inherited from below.
//	if err != nil {
//	    return errtrail.Wrap(err, "load profile")
//	}
//
//	// At the boundary (an HTTP handler): log everything, return only the public message.
//	slog.ErrorContext(ctx, "request failed", slog.Any("error", err))
//	_ = problem.Write(w, err) // RFC 9457 response
//
// The three layers each have one job: the source classifies (attaches a
// Code), middle layers add context (Wrap), and the boundary logs once and
// responds. Get these right and the HTTP/gRPC status, client message, and
// retry decision all follow from the Code.
//
// Guidelines:
//
//   - Attach the Code at the source, once — it is the single source of
//     truth. Wrap to add context; don't reclassify in the middle unless you
//     are deliberately translating one failure into another (WithCode).
//   - Keep internal and public strictly separate. Exactly four channels
//     reach a client: WithPublic, WithPublicField, WithFieldViolation, and
//     WithRetryDelay; the internal message and With attrs are for logs.
//     When unsure, leave WithPublic unset — clients get the generic status
//     text (HTTP) or the code name (gRPC) instead of a leaked detail.
//   - Classify with CodeOf; errtrail does not overload errors.Is for codes.
//     errors.Is/As still work for sentinel values, since Wrap keeps the cause.
//   - Log once, at the boundary, via slog.Any — LogValue expands code, trace,
//     and attrs, and deliberately omits the public message.
//   - Register custom codes from init. Registration is safe at any time
//     (the registry swaps atomically), but init keeps the taxonomy identical
//     for every request. Give each code a unique SCREAMING_SNAKE name.
//   - Wrap returns *Error, not error: don't return it unconditionally from a
//     function typed to return error, or a nil result becomes a non-nil error.
//
// Use the problem subpackage for HTTP responses and the grpcerr submodule
// for gRPC conversion.
package errtrail
