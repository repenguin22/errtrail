// Package problem converts errtrail errors into RFC 9457 (Problem Details
// for HTTP APIs) JSON responses. It never includes the internal message,
// attrs, or trace — clients only ever see the explicitly-set public message
// (errtrail.LookupPublicMessage, emitted as the detail member; the title
// carries the generic status wording) and the public fields
// (errtrail.PublicFields, emitted as extension members).
package problem

import (
	"encoding/json"
	"net/http"

	"github.com/repenguin22/errtrail"
)

// Problem is an RFC 9457 Problem Details object. Code is an extension
// member (RFC 9457 §3.2) that conveys errtrail's Code name to clients in a
// machine-readable form.
type Problem struct {
	Type     string `json:"type,omitempty"` // Omitted means "about:blank", per RFC.
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"` // URI for this specific occurrence.
	Code     string `json:"code"`

	// Extensions holds additional extension members (RFC 9457 §3.2),
	// flattened into the top-level JSON object by MarshalJSON. From fills
	// it with errtrail.PublicFields(err). Entries whose key is empty or
	// collides with a defined member (type, title, status, detail,
	// instance, code) are silently dropped rather than allowed to corrupt
	// the defined members.
	Extensions map[string]any `json:"-"`
}

// reservedKeys are the members serialized from Problem's struct fields;
// Extensions entries with these keys are dropped.
var reservedKeys = map[string]bool{
	"type": true, "title": true, "status": true,
	"detail": true, "instance": true, "code": true,
}

// MarshalJSON flattens Extensions into the top-level object alongside the
// defined members. The explicit field list below must be kept in sync with
// the Problem struct. encoding/json sorts map keys, so output is
// deterministic.
//
// It must stay a value receiver despite the copy cost: json.Marshal is
// called on Problem values (e.g. json.Marshal(From(err))), which would
// silently skip a pointer-receiver MarshalJSON.
//
//nolint:gocritic // hugeParam — value receiver is required, see above.
func (p Problem) MarshalJSON() ([]byte, error) {
	m := make(map[string]any, 6+len(p.Extensions))
	for k, v := range p.Extensions {
		if k == "" || reservedKeys[k] {
			continue
		}
		m[k] = v
	}
	if p.Type != "" {
		m["type"] = p.Type
	}
	m["title"] = p.Title
	m["status"] = p.Status
	if p.Detail != "" {
		m["detail"] = p.Detail
	}
	if p.Instance != "" {
		m["instance"] = p.Instance
	}
	m["code"] = p.Code
	return json.Marshal(m)
}

// Option customizes the Problem built by From. Options are applied last,
// after every derived member is set.
type Option func(*Problem)

// Instance returns an Option that sets the problem's instance member — a
// URI identifying this specific occurrence, typically the request path.
// It is boundary information (from the request), which is why it's an
// Option here rather than something stored on the error.
func Instance(uri string) Option {
	return func(p *Problem) { p.Instance = uri }
}

// TypeURL is an optional hook that derives a type URI from a Code. If you
// set it, do so before the server starts — writing it afterward races with
// concurrent reads.
var TypeURL func(errtrail.Code) string

// From builds a Problem from err. It never includes internal information —
// extension members come only from data explicitly marked public via
// errtrail's WithPublicField, never from attrs or internal messages.
//
//	Status     = errtrail.CodeOf(err).HTTPStatus()
//	Title      = http.StatusText(Status), or the code name when http.StatusText
//	             does not know the status (e.g. Canceled's 499)
//	Detail     = the explicitly-set public message (errtrail.LookupPublicMessage),
//	             or empty if none is set or it equals Title (avoids redundancy)
//	Code       = errtrail.CodeOf(err).String()
//	Type       = TypeURL(code) if TypeURL is set, otherwise empty
//	Extensions = errtrail.PublicFields(err), plus — when the error carries
//	             field violations (errtrail.WithFieldViolation) — an "errors"
//	             member holding them as [{"field", "description"}, ...]. An
//	             explicit WithPublicField("errors", ...) wins over the derived
//	             member (explicit beats derived — even an explicit nil value
//	             suppresses it, serializing as "errors": null).
//
// Options (e.g. Instance) are applied last. Problem responses describe
// errors; passing a nil err yields a 200 "OK" problem, which is almost
// certainly a caller bug.
func From(err error, opts ...Option) Problem {
	code := errtrail.CodeOf(err)
	status := code.HTTPStatus()
	title := http.StatusText(status)
	if title == "" {
		// http.StatusText knows nothing about non-standard statuses such as
		// Canceled's 499, and RFC 9457 expects title to be a human-readable
		// summary — fall back to the code name rather than leaving it empty.
		title = code.String()
	}

	// Only an explicitly-set public message becomes the detail; the client
	// already gets the generic wording via title, so no fallback is needed
	// (and a public message equal to the title is dropped as redundant).
	detail, _ := errtrail.LookupPublicMessage(err)
	if detail == title {
		detail = ""
	}

	extensions := errtrail.PublicFields(err)
	if vs := errtrail.FieldViolations(err); len(vs) > 0 {
		if extensions == nil {
			extensions = make(map[string]any, 1)
		}
		// An explicit public field named "errors" wins over the derived
		// member — explicit beats derived.
		if _, ok := extensions["errors"]; !ok {
			extensions["errors"] = vs
		}
	}

	p := Problem{
		Title:      title,
		Status:     status,
		Detail:     detail,
		Code:       code.String(),
		Extensions: extensions,
	}
	if TypeURL != nil {
		p.Type = TypeURL(code)
	}
	for _, o := range opts {
		o(&p)
	}
	return p
}

// Write writes From(err, opts...) to w as application/problem+json. If JSON
// encoding fails (possible when a public field holds a value
// encoding/json cannot marshal), it writes a bare 500 and returns that
// error.
func Write(w http.ResponseWriter, err error, opts ...Option) error {
	p := From(err, opts...)
	body, mErr := json.Marshal(p)
	if mErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return mErr
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_, wErr := w.Write(body)
	return wErr
}
