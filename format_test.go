package errtrail

import (
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

func TestFormatVAndS(t *testing.T) {
	e := Wrap(errors.New("sql: no rows"), "query user")
	want := "query user: sql: no rows"
	if got := fmt.Sprintf("%v", e); got != want {
		t.Errorf("%%v = %q, want %q", got, want)
	}
	if got := fmt.Sprintf("%s", e); got != want {
		t.Errorf("%%s = %q, want %q", got, want)
	}
}

func TestFormatQ(t *testing.T) {
	e := New(NotFound, "boom")
	if got := fmt.Sprintf("%q", e); got != `"boom"` {
		t.Errorf("%%q = %q", got)
	}
}

func TestFormatPlusV(t *testing.T) {
	inner := New(NotFound, "query user").WithPublic("User not found").
		WithPublicField("resource", "user")
	outer := Wrap(inner, "get profile").With(slog.Int("user_id", 42))

	out := fmt.Sprintf("%+v", outer)

	// The first line is the concatenated message.
	if !strings.HasPrefix(out, "get profile: query user\n") {
		t.Errorf("first line wrong:\n%s", out)
	}
	mustContain(t, out, "\n  code: NOT_FOUND")
	mustContain(t, out, "\n  public: User not found")
	mustContain(t, out, "\n  public.fields: resource=user")
	mustContain(t, out, "\n  attrs: user_id=42")
	mustContain(t, out, "\n  trace:")

	// Trace lines look like "Func (file:line): msg".
	traceLine := regexp.MustCompile(`\n {4}\S+ \(.+:\d+\): get profile`)
	if !traceLine.MatchString(out) {
		t.Errorf("trace line for outer not found:\n%s", out)
	}
}

func TestFormatPlusVViolations(t *testing.T) {
	e := New(InvalidArgument, "bad request").
		WithFieldViolation("email", "must be valid").
		WithFieldViolation("age", "must be >= 0")
	out := fmt.Sprintf("%+v", e)
	mustContain(t, out, "\n  public.violations: email=must be valid age=must be >= 0")

	// Below a barrier the line disappears, matching FieldViolations.
	blocked := Wrap(e, "reclassify").WithoutPublic()
	if out := fmt.Sprintf("%+v", blocked); strings.Contains(out, "public.violations:") {
		t.Errorf("blocked violations printed:\n%s", out)
	}
}

func TestFormatPlusVOmitsUnsetSections(t *testing.T) {
	e := New(Internal, "boom") // no public, no fields, no attrs
	out := fmt.Sprintf("%+v", e)
	if strings.Contains(out, "public:") {
		t.Error("public line should be omitted when unset")
	}
	if strings.Contains(out, "public.fields:") {
		t.Error("public.fields line should be omitted when empty")
	}
	if strings.Contains(out, "public.violations:") {
		t.Error("public.violations line should be omitted when empty")
	}
	if strings.Contains(out, "attrs:") {
		t.Error("attrs line should be omitted when empty")
	}
	// trace is always present, since New records one frame.
	mustContain(t, out, "\n  trace:")
	mustContain(t, out, "\n  code: INTERNAL")
}

func TestFormatPlusVWithoutPublic(t *testing.T) {
	inner := New(NotFound, "query user").WithPublic("User not found").
		WithPublicField("resource", "user")
	outer := Wrap(inner, "reclassify").WithCode(PermissionDenied).WithoutPublic().
		With(slog.Int("user_id", 42))

	out := fmt.Sprintf("%+v", outer)

	// The public lines show what a client can actually see — nothing, here.
	if strings.Contains(out, "public:") {
		t.Errorf("blocked public message printed:\n%s", out)
	}
	if strings.Contains(out, "public.fields:") {
		t.Errorf("blocked public fields printed:\n%s", out)
	}
	// Internal sections are unaffected by the barrier.
	mustContain(t, out, "\n  code: PERMISSION_DENIED")
	mustContain(t, out, "\n  attrs: user_id=42")
	mustContain(t, out, "\n  trace:")
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("output missing %q:\n%s", sub, s)
	}
}
