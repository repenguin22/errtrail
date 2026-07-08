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
