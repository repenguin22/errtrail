// Package problem converts errtrail errors into RFC 9457 (Problem Details
// for HTTP APIs) JSON responses. It never includes the internal message,
// attrs, or trace — clients only ever see the public message
// (errtrail.PublicMessage).
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
	Type   string `json:"type,omitempty"` // Omitted means "about:blank", per RFC.
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code"`
}

// TypeURL is an optional hook that derives a type URI from a Code. If you
// set it, do so before the server starts — writing it afterward races with
// concurrent reads.
var TypeURL func(errtrail.Code) string

// From builds a Problem from err. It never includes internal information.
//
//	Status = errtrail.CodeOf(err).HTTPStatus()
//	Title  = http.StatusText(Status)
//	Detail = errtrail.PublicMessage(err), or empty if it equals Title (avoids redundancy)
//	Code   = errtrail.CodeOf(err).String()
//	Type   = TypeURL(code) if TypeURL is set, otherwise empty
func From(err error) Problem {
	code := errtrail.CodeOf(err)
	status := code.HTTPStatus()
	title := http.StatusText(status)

	detail := errtrail.PublicMessage(err)
	if detail == title {
		detail = ""
	}

	p := Problem{
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code.String(),
	}
	if TypeURL != nil {
		p.Type = TypeURL(code)
	}
	return p
}

// Write writes From(err) to w as application/problem+json. If JSON encoding
// fails, it writes the status and returns that error.
func Write(w http.ResponseWriter, err error) error {
	p := From(err)
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
