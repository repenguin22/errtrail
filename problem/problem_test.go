package problem

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	// public not set -> PublicMessage falls back to http.StatusText = "Not Found" = Title.
	err := errtrail.New(errtrail.NotFound, "internal")
	p := From(err)
	if p.Detail != "" {
		t.Errorf("Detail = %q, want empty (equals title)", p.Detail)
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
