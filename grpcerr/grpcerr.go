// Package grpcerr converts errtrail errors into gRPC's *status.Status. It
// depends on google.golang.org/grpc, so it's kept in a module separate from
// the errtrail core — users who don't need gRPC never import this package
// and never pull in that dependency.
package grpcerr

import (
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/repenguin22/errtrail"
)

// Domain opts in to attaching an errdetails.ErrorInfo to every non-OK
// status produced by ToStatus / ToError, carrying the errtrail code name
// across the wire (see ToStatus). Set it to the service's domain (e.g.
// "myservice.example.com") before the server starts — writing it afterward
// races with concurrent reads. Empty (the default) attaches nothing.
var Domain string

// ToStatus converts err to a *status.Status.
//
//	Code    = codes.Code(errtrail.CodeOf(err).GRPCCode())
//	Message = the explicitly-set public message (errtrail.LookupPublicMessage),
//	          or the code name (e.g. "UNAVAILABLE", "RATE_LIMITED") when none is set
//
// The code-name fallback keeps HTTP wording ("Internal Server Error") off the
// gRPC wire and guarantees a non-empty message even for codes whose HTTP
// status has no standard text (Canceled's 499, custom codes).
//
// When Domain is non-empty, an errdetails.ErrorInfo{Reason: code.String(),
// Domain: Domain} is attached, so clients receive the errtrail code name
// (e.g. "RATE_LIMITED") even when several custom codes share one numeric
// gRPC code. Reason is the code's String() form — for an unregistered code
// that is "CODE(123)", which is not AIP-193-shaped but preserves the
// number. If the details cannot be attached (a proto marshal failure), the
// plain status is returned instead.
//
// Returns status.New(codes.OK, "") when err is nil. Never includes the
// internal message, attrs, or trace. Callers who need further details
// (RetryInfo, BadRequest, ...) can call WithDetails on the returned status
// themselves.
func ToStatus(err error) *status.Status {
	if err == nil {
		return status.New(codes.OK, "")
	}
	code := errtrail.CodeOf(err)
	msg, ok := errtrail.LookupPublicMessage(err)
	if !ok {
		msg = code.String()
	}
	st := status.New(codes.Code(code.GRPCCode()), msg)
	if Domain == "" {
		return st
	}
	return withErrorInfo(st, code)
}

// withErrorInfo attaches an errdetails.ErrorInfo{Reason: code.String(),
// Domain: Domain} to st. If the details cannot be attached (WithDetails
// rejects OK statuses and surfaces proto marshal failures), the plain status
// is returned instead — never lose the status itself over details.
func withErrorInfo(st *status.Status, code errtrail.Code) *status.Status {
	detailed, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: code.String(),
		Domain: Domain,
	})
	if err != nil {
		return st
	}
	return detailed
}

// ToError returns ToStatus(err).Err(), for returning directly from a gRPC
// handler. Returns nil when err is nil.
func ToError(err error) error {
	if err == nil {
		return nil
	}
	return ToStatus(err).Err()
}

// FromError converts an error returned by a gRPC call into an
// *errtrail.Error that wraps it, so callers share the same Code taxonomy
// end to end and errors.Is / errors.As keep seeing the original error.
//
// Returns nil when err is nil. The wire code (0–16) maps to the errtrail
// Code one-to-one; codes above 16 and non-status errors become Unknown. A
// custom code is recovered from an errdetails.ErrorInfo detail whose Reason
// names a locally registered code AND whose registered gRPC code matches
// the wire code — the second condition guards against a foreign service's
// taxonomy that happens to reuse a local code name.
//
// The wire message survives as the internal message via the wrapped cause;
// it is NOT set as the public message (call WithPublic explicitly to
// propagate it to your own clients). The recorded frame points inside
// grpcerr — wrap the result with errtrail.Wrap at the call site to add a
// caller frame.
func FromError(err error) *errtrail.Error {
	if err == nil {
		return nil
	}
	// Non-status errors yield status.New(codes.Unknown, err.Error()).
	st, _ := status.FromError(err)
	return errtrail.Wrap(err, "").WithCode(codeFromStatus(st))
}

// FromStatus is FromError for a *status.Status you already hold. Returns
// nil when st is nil or its code is OK (st.Err() is nil in both cases).
func FromStatus(st *status.Status) *errtrail.Error {
	return errtrail.Wrap(st.Err(), "").WithCode(codeFromStatus(st))
}

// codeFromStatus maps a status to an errtrail Code: wire codes 0–16 map
// one-to-one, anything else is Unknown, and an ErrorInfo detail may recover
// a registered custom code (see FromError). Safe on a nil status (OK).
func codeFromStatus(st *status.Status) errtrail.Code {
	wire := uint32(st.Code())
	code := errtrail.Unknown
	if wire <= 16 {
		code = errtrail.Code(wire)
	}
	for _, d := range st.Details() {
		info, ok := d.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		if c, ok := errtrail.CodeByName(info.GetReason()); ok && c.GRPCCode() == wire {
			return c
		}
	}
	return code
}
