// Package grpcerr converts errtrail errors into gRPC's *status.Status. It
// depends on google.golang.org/grpc, so it's kept in a module separate from
// the errtrail core — users who don't need gRPC never import this package
// and never pull in that dependency.
package grpcerr

import (
	"slices"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/durationpb"

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
// gRPC code. The detail is attached only for registered codes (those
// errtrail.CodeByName resolves): an unregistered code's "CODE(123)" form
// violates the ErrorInfo.Reason spec and cannot round-trip anyway, so such
// a status ships plain.
//
// Independent of Domain, two further details are attached when the error
// carries the data for them: an errdetails.RetryInfo with the delay of a
// code registered with errtrail.RetryAfter (readable on the client via
// RetryDelay), and an errdetails.BadRequest built from the error's field
// violations (errtrail.WithFieldViolation). Errors without that data keep
// today's wire format exactly. If a detail cannot be attached (a proto
// marshal failure — e.g. invalid UTF-8 in a user-derived violation string),
// only that detail is dropped; the others and the status itself survive.
//
// The public MESSAGE sits outside that isolation: it lives on the Status
// proto itself, and invalid UTF-8 there makes the transport unable to
// marshal the grpc-status-details-bin trailer — the client receives the
// code and message but ZERO details, and only grpclog sees the failure
// (the application gets no signal). Validate with utf8.ValidString before
// echoing user input into WithPublic.
//
// Returns status.New(codes.OK, "") when err is nil. Never includes the
// internal message, attrs, or trace. Callers who need other details can
// still call WithDetails on the returned status themselves.
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
	if details := detailsFor(err, code); len(details) > 0 {
		return withDetails(st, details)
	}
	return st
}

// detailsFor collects the standard error details derived from err and its
// resolved code: an ErrorInfo when Domain is set and the code is registered
// (see ToStatus), a RetryInfo when the code was registered with RetryAfter,
// and a BadRequest when the error carries field violations. The order is
// fixed: ErrorInfo, RetryInfo, BadRequest.
func detailsFor(err error, code errtrail.Code) []protoadapt.MessageV1 {
	var details []protoadapt.MessageV1
	if Domain != "" {
		if _, ok := errtrail.CodeByName(code.String()); ok {
			details = append(details, &errdetails.ErrorInfo{
				Reason: code.String(),
				Domain: Domain,
			})
		}
	}
	if d, ok := code.RetryDelay(); ok {
		details = append(details, &errdetails.RetryInfo{RetryDelay: durationpb.New(d)})
	}
	if vs := errtrail.FieldViolations(err); len(vs) > 0 {
		fv := make([]*errdetails.BadRequest_FieldViolation, len(vs))
		for i, v := range vs {
			fv[i] = &errdetails.BadRequest_FieldViolation{
				Field:       v.Field,
				Description: v.Description,
			}
		}
		details = append(details, &errdetails.BadRequest{FieldViolations: fv})
	}
	return details
}

// withDetails attaches details to st. The batch attach is tried first (one
// call on the happy path); if it fails (WithDetails rejects OK statuses and
// surfaces proto marshal failures — e.g. invalid UTF-8 in a user-derived
// violation string, which proto3 refuses to marshal), each detail is
// retried individually so a poisoned detail costs only itself, in the fixed
// priority order (ErrorInfo, RetryInfo, BadRequest). A detail is atomic —
// its contents are never partially rewritten. The status itself is never
// lost over details.
func withDetails(st *status.Status, details []protoadapt.MessageV1) *status.Status {
	detailed, err := st.WithDetails(details...)
	if err == nil {
		return detailed
	}
	for _, d := range details {
		if next, err := st.WithDetails(d); err == nil {
			st = next
		}
	}
	return st
}

// ToError returns ToStatus(err).Err(), for returning directly from a gRPC
// handler. Returns nil when err is nil.
func ToError(err error) error {
	if err == nil {
		return nil
	}
	return ToStatus(err).Err()
}

// FromOption configures FromError / FromStatus. The zero set of options
// keeps the default recovery rules.
type FromOption func(*fromOptions)

// fromOptions is the folded option set for one FromError / FromStatus call.
type fromOptions struct {
	trustedDomains []string // empty = no domain restriction (the default)
}

// TrustedDomain returns a FromOption that additionally requires a custom
// code's ErrorInfo.Domain to equal one of domains before the code is
// recovered; a detail from any other domain degrades to the numeric wire
// code, exactly as if the detail were absent. Use it when the client also
// talks to services outside your own taxonomy — the default name+numeric
// double check cannot tell a foreign service's ErrorInfo apart from yours
// if both the Reason and the numeric code happen to match.
//
// Passing several domains (or the option several times) trusts any of
// them. With no arguments it is a no-op.
func TrustedDomain(domains ...string) FromOption {
	return func(o *fromOptions) { o.trustedDomains = append(o.trustedDomains, domains...) }
}

// FromError converts an error returned by a gRPC call into an
// *errtrail.Error that wraps it, so callers share the same Code taxonomy
// end to end and errors.Is / errors.As keep seeing the original error.
//
// Returns nil when err is nil. Caution: the return type is *Error, so the
// same typed-nil footgun Wrap has applies — returning the result directly
// from a function declared to return error yields a non-nil error holding a
// typed nil when err was nil; keep the usual `if err != nil` guard.
//
// The wire code (0–16) maps to the errtrail Code one-to-one; codes above 16
// and non-status errors become Unknown. A custom code is recovered from an
// errdetails.ErrorInfo detail whose Reason names a locally registered code
// AND whose registered gRPC code matches the wire code — the second
// condition guards against a foreign service's taxonomy that happens to
// reuse a local code name. The default does not inspect ErrorInfo.Domain;
// pass TrustedDomain to require a domain match as well.
//
// The wire message survives as the internal message via the wrapped cause;
// it is NOT set as the public message (call WithPublic explicitly to
// propagate it to your own clients — but note the wire message can carry
// raw non-UTF-8 bytes, which grpc-message percent-decoding restores;
// echoing it unvalidated into WithPublic poisons your own response's
// details, see ToStatus). For the same reason received details
// are not turned back into public data on the returned error — a
// downstream's BadRequest violations are its clients' business, not
// automatically yours; read a RetryInfo delay with RetryDelay. The recorded
// frame points inside grpcerr — wrap the result with errtrail.Wrap at the
// call site to add a caller frame.
func FromError(err error, opts ...FromOption) *errtrail.Error {
	if err == nil {
		return nil
	}
	// Non-status errors yield status.New(codes.Unknown, err.Error()).
	st, _ := status.FromError(err)
	return errtrail.Wrap(err, "").WithCode(codeFromStatus(st, foldFromOptions(opts)))
}

// FromStatus is FromError for a *status.Status you already hold. Returns
// nil when st is nil or its code is OK (st.Err() is nil for an OK status) —
// the typed-nil caveat on FromError applies here too.
func FromStatus(st *status.Status, opts ...FromOption) *errtrail.Error {
	// Explicit guard rather than relying on grpc's nil-receiver behavior of
	// Status.Err/Code/Details — that is an implementation detail, not a
	// documented guarantee.
	if st == nil {
		return nil
	}
	return errtrail.Wrap(st.Err(), "").WithCode(codeFromStatus(st, foldFromOptions(opts)))
}

// foldFromOptions applies opts onto a zero fromOptions.
func foldFromOptions(opts []FromOption) fromOptions {
	var o fromOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// RetryDelay returns the retry delay carried by the first
// errdetails.RetryInfo detail on err's gRPC status that holds a valid,
// positive delay, reporting whether one was found. It pairs with the server
// side registering a code with errtrail.RetryAfter, but reads any RetryInfo
// regardless of origin. Returns (0, false) for nil, non-status errors,
// statuses without a RetryInfo detail, and — since "retry after zero"
// carries no recommendation — RetryInfo details whose delay is unset, zero,
// negative, or outside the protobuf Duration range (AsDuration would
// silently saturate an out-of-range value to ±292 years; a foreign service
// may attach such a detail, while errtrail's own RetryAfter only registers
// positive in-range delays).
//
// Like IsRetryable (which stays derived from the Code alone), the delay is
// a hint — honoring it, idempotency, and retry budgets remain the caller's
// responsibility.
func RetryDelay(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	// Non-status errors yield a detail-less Unknown status, so they fall
	// through to (0, false).
	st, _ := status.FromError(err)
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.RetryInfo); ok {
			rd := info.GetRetryDelay()
			if rd.CheckValid() != nil {
				continue // nil or outside the protobuf Duration range
			}
			if delay := rd.AsDuration(); delay > 0 {
				return delay, true
			}
		}
	}
	return 0, false
}

// codeFromStatus maps a status to an errtrail Code: wire codes 0–16 map
// one-to-one, anything else is Unknown, and an ErrorInfo detail may recover
// a registered custom code (see FromError; o.trustedDomains, when set, also
// requires the detail's Domain to match). Safe on a nil status (OK).
func codeFromStatus(st *status.Status, o fromOptions) errtrail.Code {
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
		if len(o.trustedDomains) > 0 && !slices.Contains(o.trustedDomains, info.GetDomain()) {
			continue
		}
		if c, ok := errtrail.CodeByName(info.GetReason()); ok && c.GRPCCode() == wire {
			return c
		}
	}
	return code
}
