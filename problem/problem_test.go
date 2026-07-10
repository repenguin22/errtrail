package problem

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/repenguin22/errtrail"
)

func TestFromWithPublic(t *testing.T) {
	err := errtrail.New(errtrail.NotFound, "row missing").WithPublic("User not found")
	p := From(err)
	if p.Status != 404 {
		t.Errorf("Status = %d", p.Status)
	}
	if p.Title != "Not Found" {
		t.Errorf("Title = %q", p.Title)
	}
	if p.Detail != "User not found" {
		t.Errorf("Detail = %q", p.Detail)
	}
	if p.Code != "NOT_FOUND" {
		t.Errorf("Code = %q", p.Code)
	}
	if p.Type != "" {
		t.Errorf("Type = %q, want empty", p.Type)
	}
}

func TestFromDetailOmittedWhenEqualTitle(t *testing.T) {
	// An explicit public message equal to the title is dropped as redundant.
	err := errtrail.New(errtrail.NotFound, "internal").WithPublic("Not Found")
	p := From(err)
	if p.Detail != "" {
		t.Errorf("Detail = %q, want empty (equals title)", p.Detail)
	}

	// With no public message set at all, the detail also stays empty — the
	// title already carries the generic wording (From uses
	// LookupPublicMessage, which never falls back).
	if p := From(errtrail.New(errtrail.NotFound, "internal")); p.Detail != "" {
		t.Errorf("Detail = %q, want empty (no public message)", p.Detail)
	}
}

func TestFromTitleFallsBackToCodeName(t *testing.T) {
	// http.StatusText(499) is "", so the title falls back to the code name.
	err := errtrail.New(errtrail.Canceled, "ctx canceled")
	p := From(err)
	if p.Status != 499 {
		t.Errorf("Status = %d, want 499", p.Status)
	}
	if p.Title != "CANCELED" {
		t.Errorf("Title = %q, want CANCELED", p.Title)
	}
}

func TestFromNeverLeaksInternal(t *testing.T) {
	err := errtrail.New(errtrail.Internal, "db password = hunter2")
	p := From(err)
	if p.Detail == "db password = hunter2" {
		t.Error("Detail leaked internal message")
	}
}

func TestFromWithoutPublicBarrier(t *testing.T) {
	// Reclassifying with WithoutPublic: the inner public message and fields
	// must not surface in the problem response.
	inner := errtrail.New(errtrail.NotFound, "row missing").
		WithPublic("User not found").
		WithPublicField("user_id", "42")
	err := errtrail.Wrap(inner, "reclassify").
		WithCode(errtrail.PermissionDenied).WithoutPublic()

	p := From(err)
	if p.Status != 403 {
		t.Errorf("Status = %d, want 403", p.Status)
	}
	if p.Detail != "" {
		t.Errorf("Detail = %q, want empty (blocked)", p.Detail)
	}
	if p.Extensions != nil {
		t.Errorf("Extensions = %v, want nil (blocked)", p.Extensions)
	}
}

func TestTypeURLHook(t *testing.T) {
	TypeURL = func(c errtrail.Code) string {
		return "https://errors.example.com/" + c.String()
	}
	t.Cleanup(func() { TypeURL = nil })

	err := errtrail.New(errtrail.NotFound, "x")
	p := From(err)
	if p.Type != "https://errors.example.com/NOT_FOUND" {
		t.Errorf("Type = %q", p.Type)
	}
}

func TestWrite(t *testing.T) {
	err := errtrail.New(errtrail.NotFound, "internal").WithPublic("User not found")
	rec := httptest.NewRecorder()

	if wErr := Write(rec, err); wErr != nil {
		t.Fatalf("Write returned error: %v", wErr)
	}

	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d", rec.Code)
	}

	var p Problem
	if e := json.Unmarshal(rec.Body.Bytes(), &p); e != nil {
		t.Fatalf("invalid json: %v", e)
	}
	if p.Detail != "User not found" || p.Code != "NOT_FOUND" {
		t.Errorf("body = %+v", p)
	}
}

func TestFromExtensionsFlattened(t *testing.T) {
	err := errtrail.New(errtrail.InvalidArgument, "bad email").
		WithPublicField("field", "email").
		WithPublicField("errors", []map[string]string{{"detail": "must be valid", "pointer": "#/email"}})
	p := From(err)

	body, mErr := json.Marshal(p)
	if mErr != nil {
		t.Fatalf("marshal: %v", mErr)
	}
	var raw map[string]any
	if e := json.Unmarshal(body, &raw); e != nil {
		t.Fatal(e)
	}
	// Extension members appear at the top level, alongside defined members.
	if raw["field"] != "email" {
		t.Errorf("field = %v", raw["field"])
	}
	if _, ok := raw["errors"].([]any); !ok {
		t.Errorf("errors = %v (%T)", raw["errors"], raw["errors"])
	}
	if raw["status"] != float64(400) || raw["code"] != "INVALID_ARGUMENT" {
		t.Errorf("defined members broken: %v", raw)
	}
}

func TestFromFieldViolations(t *testing.T) {
	err := errtrail.New(errtrail.InvalidArgument, "bad request").
		WithPublic("Validation failed").
		WithFieldViolation("email", "must be a valid email address").
		WithFieldViolation("age", "must be at least 0")

	body, mErr := json.Marshal(From(err))
	if mErr != nil {
		t.Fatalf("marshal: %v", mErr)
	}
	var raw map[string]any
	if e := json.Unmarshal(body, &raw); e != nil {
		t.Fatal(e)
	}
	errs, ok := raw["errors"].([]any)
	if !ok || len(errs) != 2 {
		t.Fatalf(`errors = %v (%T), want 2 entries`, raw["errors"], raw["errors"])
	}
	first, _ := errs[0].(map[string]any)
	if first["field"] != "email" || first["description"] != "must be a valid email address" {
		t.Errorf("errors[0] = %v", errs[0])
	}

	// Without violations, the key is absent entirely.
	if p := From(errtrail.New(errtrail.InvalidArgument, "x")); p.Extensions != nil {
		t.Errorf("Extensions = %v, want nil without violations", p.Extensions)
	}
}

func TestFromExplicitErrorsFieldWins(t *testing.T) {
	// An explicit WithPublicField("errors", ...) overrides the derived
	// member built from field violations.
	err := errtrail.New(errtrail.InvalidArgument, "x").
		WithPublicField("errors", "explicit").
		WithFieldViolation("email", "invalid")
	p := From(err)
	if p.Extensions["errors"] != "explicit" {
		t.Errorf(`Extensions["errors"] = %v, want the explicit public field`, p.Extensions["errors"])
	}
}

func TestMarshalDropsReservedAndEmptyExtensionKeys(t *testing.T) {
	err := errtrail.New(errtrail.InvalidArgument, "x").
		WithPublicField("status", 999). // reserved — must not corrupt the real status
		WithPublicField("", "no key").  // empty — dropped
		WithPublicField("ok", "kept")
	body, mErr := json.Marshal(From(err))
	if mErr != nil {
		t.Fatalf("marshal: %v", mErr)
	}
	var raw map[string]any
	if e := json.Unmarshal(body, &raw); e != nil {
		t.Fatal(e)
	}
	if raw["status"] != float64(400) {
		t.Errorf("status = %v, want 400 (reserved key must win)", raw["status"])
	}
	if _, ok := raw[""]; ok {
		t.Error("empty extension key should be dropped")
	}
	if raw["ok"] != "kept" {
		t.Errorf("ok = %v", raw["ok"])
	}
}

func TestInstanceOption(t *testing.T) {
	err := errtrail.New(errtrail.NotFound, "x")

	p := From(err, Instance("/users/42"))
	if p.Instance != "/users/42" {
		t.Errorf("Instance = %q", p.Instance)
	}

	// Without the option, the instance key is absent from the JSON entirely.
	body, mErr := json.Marshal(From(err))
	if mErr != nil {
		t.Fatalf("marshal: %v", mErr)
	}
	var raw map[string]any
	if e := json.Unmarshal(body, &raw); e != nil {
		t.Fatal(e)
	}
	if _, ok := raw["instance"]; ok {
		t.Error("instance should be omitted when unset")
	}
}

func TestWritePassesOptions(t *testing.T) {
	err := errtrail.New(errtrail.NotFound, "x")
	rec := httptest.NewRecorder()
	if wErr := Write(rec, err, Instance("/users/42")); wErr != nil {
		t.Fatalf("Write returned error: %v", wErr)
	}
	var raw map[string]any
	if e := json.Unmarshal(rec.Body.Bytes(), &raw); e != nil {
		t.Fatal(e)
	}
	if raw["instance"] != "/users/42" {
		t.Errorf("instance = %v", raw["instance"])
	}
}

func TestWriteUnmarshalableExtension(t *testing.T) {
	// A public field can hold a value encoding/json cannot marshal; Write
	// must fall back to a bare 500 and surface the error.
	err := errtrail.New(errtrail.Internal, "x").WithPublicField("bad", make(chan int))
	rec := httptest.NewRecorder()
	if wErr := Write(rec, err); wErr == nil {
		t.Fatal("Write should return the marshal error")
	}
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body should be empty, got %q", rec.Body.String())
	}
}

func TestWriteAdversarialNeverLeaksInternals(t *testing.T) {
	// Adversarial end-to-end check: build the nastiest realistic chain —
	// raw error with credentials, internal messages, attrs, a std fmt layer,
	// a Join branch, wraps on top — and assert the serialized HTTP body
	// contains none of it. Only the explicitly public data may appear.
	base := errors.New(`pq: password authentication failed for "svc-hunter2"`)
	inner := errtrail.Wrap(base, "query user by email").
		WithCode(errtrail.NotFound).
		With(slog.String("user_email", "leak-attr@example.com"), slog.Int("attempt", 3)).
		WithPublic("User not found").
		WithPublicField("resource", "user").
		WithFieldViolation("user_id", "unknown user")
	mid := fmt.Errorf("repo layer: %w", inner)
	sibling := errtrail.New(errtrail.Internal, "cache shard 7 corrupt")
	outer := errtrail.Wrap(errors.Join(mid, sibling), "handle profile request")

	rec := httptest.NewRecorder()
	if wErr := Write(rec, outer, Instance("/users/42")); wErr != nil {
		t.Fatalf("Write returned error: %v", wErr)
	}
	body := rec.Body.String()

	// Nothing internal: message fragments, attr keys/values, and anything a
	// serialized trace would carry (file paths, function/test names).
	leaks := []string{
		"hunter2", "password", "pq:",
		"query user by email", "repo layer", "cache shard", "handle profile request",
		"user_email", "leak-attr", "attempt",
		".go:", "problem_test", "trace",
	}
	for _, s := range leaks {
		if strings.Contains(body, s) {
			t.Errorf("body leaked internal data %q:\n%s", s, body)
		}
	}

	// Sanity: the response actually carries the public data, so the leak
	// assertions above can't pass vacuously on an empty body.
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	var raw map[string]any
	if e := json.Unmarshal(rec.Body.Bytes(), &raw); e != nil {
		t.Fatalf("invalid json: %v", e)
	}
	if raw["code"] != "NOT_FOUND" || raw["detail"] != "User not found" ||
		raw["resource"] != "user" || raw["instance"] != "/users/42" {
		t.Errorf("public data missing from body: %s", body)
	}
	// Field violations are public data — present, with only Field/Description.
	if errs, ok := raw["errors"].([]any); !ok || len(errs) != 1 {
		t.Errorf("errors = %v, want the one violation", raw["errors"])
	}
}

func TestWriteJSONTagOmitsEmptyType(t *testing.T) {
	err := errtrail.New(errtrail.Internal, "x")
	rec := httptest.NewRecorder()
	_ = Write(rec, err)

	var raw map[string]any
	if e := json.Unmarshal(rec.Body.Bytes(), &raw); e != nil {
		t.Fatal(e)
	}
	if _, ok := raw["type"]; ok {
		t.Error("empty type should be omitted from JSON")
	}
	// code should always be present.
	if _, ok := raw["code"]; !ok {
		t.Error("code should always be present")
	}
}
