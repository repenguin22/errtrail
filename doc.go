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
//     costs roughly 100–150ns / 1 alloc).
//   - Fully compatible with the standard errors package (Is / As / Unwrap /
//     Join).
//   - Internal messages (for logs) are separated from public messages (for
//     clients).
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
// Use the problem subpackage for HTTP responses and the grpcerr submodule
// for gRPC conversion.
package errtrail
