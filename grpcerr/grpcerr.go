// Package grpcerr converts errtrail errors into gRPC's *status.Status. It
// depends on google.golang.org/grpc, so it's kept in a module separate from
// the errtrail core — users who don't need gRPC never import this package
// and never pull in that dependency.
package grpcerr

import (
	"github.com/repenguin22/errtrail"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ToStatus converts err to a *status.Status.
//
//	Code    = codes.Code(errtrail.CodeOf(err).GRPCCode())
//	Message = errtrail.PublicMessage(err)
//
// Returns status.New(codes.OK, "") when err is nil. Never includes the
// internal message, attrs, or trace.
func ToStatus(err error) *status.Status {
	if err == nil {
		return status.New(codes.OK, "")
	}
	c := codes.Code(errtrail.CodeOf(err).GRPCCode())
	return status.New(c, errtrail.PublicMessage(err))
}

// ToError returns ToStatus(err).Err(), for returning directly from a gRPC
// handler. Returns nil when err is nil.
func ToError(err error) error {
	if err == nil {
		return nil
	}
	return ToStatus(err).Err()
}
