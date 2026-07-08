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
//	Message = errtrail.PublicMessage(err)
//
// When Domain is non-empty, an errdetails.ErrorInfo{Reason: code.String(),
// Domain: Domain} is attached, so clients receive the errtrail code name
// (e.g. "RATE_LIMITED") even when several custom codes share one numeric
// gRPC code. Reason is the code's String() form — for an unregistered code
// that is "CODE(123)", which is not AIP-193-shaped but preserves the
// number. If the details cannot be attached (e.g. a custom code registered
// with gRPC code OK), the plain status is returned instead.
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
	st := status.New(codes.Code(code.GRPCCode()), errtrail.PublicMessage(err))
	if Domain == "" {
		return st
	}
	detailed, dErr := st.WithDetails(&errdetails.ErrorInfo{
		Reason: code.String(),
		Domain: Domain,
	})
	if dErr != nil {
		// WithDetails fails on an OK status (a custom code may be registered
		// with gRPC code OK) — never lose the status itself over details.
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
