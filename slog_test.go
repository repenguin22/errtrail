package errtrail

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

func TestLogValueJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	inner := New(NotFound, "query user").WithPublic("secret public").
		WithPublicField("client_hint", "for responses only").
		WithFieldViolation("email", "for responses only too").
		WithRetryDelay(37 * time.Second)
	err := Wrap(inner, "get profile").With(slog.Int("user_id", 42))

	logger.Error("request failed", slog.Any("error", err))

	var rec map[string]any
	if e := json.Unmarshal(buf.Bytes(), &rec); e != nil {
		t.Fatalf("invalid json: %v", e)
	}

	errObj, ok := rec["error"].(map[string]any)
	if !ok {
		t.Fatalf("error field is not an object: %v", rec["error"])
	}
	if errObj["msg"] != "get profile: query user" {
		t.Errorf("error.msg = %v", errObj["msg"])
	}
	if errObj["code"] != "NOT_FOUND" {
		t.Errorf("error.code = %v", errObj["code"])
	}
	if errObj["user_id"] != float64(42) {
		t.Errorf("error.user_id = %v", errObj["user_id"])
	}
	// public and public fields must never appear in logs.
	if _, exists := errObj["public"]; exists {
		t.Error("public must not appear in logs")
	}
	if _, exists := errObj["client_hint"]; exists {
		t.Error("public fields must not appear in logs")
	}
	if bytes.Contains(buf.Bytes(), []byte("for responses only too")) {
		t.Error("field violations must not appear in logs")
	}
	if bytes.Contains(buf.Bytes(), []byte("37s")) || bytes.Contains(buf.Bytes(), []byte("retry")) {
		t.Error("the retry delay must not appear in logs")
	}
	// trace is an array of strings.
	tr, ok := errObj["trace"].([]any)
	if !ok || len(tr) != 2 {
		t.Errorf("error.trace = %v", errObj["trace"])
	}
}

func TestLogValueDoesNotCollectPublicData(t *testing.T) {
	// LogValue never emits public fields or violations (see the LogValue
	// doc), so collecting them costs allocations for values immediately
	// discarded. Round 7 review finding: pin that a public-data-heavy
	// error costs no more allocations than a plain one.
	plain := New(Internal, "x")
	loaded := New(Internal, "x")
	for i := 0; i < 8; i++ {
		loaded = loaded.WithPublicField("f", i).WithFieldViolation("v", "bad")
	}

	n1 := testing.AllocsPerRun(50, func() { _ = plain.LogValue() })
	n2 := testing.AllocsPerRun(50, func() { _ = loaded.LogValue() })
	if n1 != n2 {
		t.Errorf("LogValue allocs: plain=%v loaded=%v, want equal (public data must not be collected)", n1, n2)
	}
}

func TestLogValueNil(t *testing.T) {
	var e *Error
	if !e.LogValue().Equal(slog.Value{}) {
		t.Error("nil LogValue should be empty")
	}
}
