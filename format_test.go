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
	inner := New(NotFound, "query user").WithPublic("User not found")
	outer := Wrap(inner, "get profile").With(slog.Int("user_id", 42))

	out := fmt.Sprintf("%+v", outer)

	// 1 行目はメッセージ連結。
	if !strings.HasPrefix(out, "get profile: query user\n") {
		t.Errorf("first line wrong:\n%s", out)
	}
	mustContain(t, out, "\n  code: NOT_FOUND")
	mustContain(t, out, "\n  public: User not found")
	mustContain(t, out, "\n  attrs: user_id=42")
	mustContain(t, out, "\n  trace:")

	// trace 行は "Func (file:line): msg" 形式。
	traceLine := regexp.MustCompile(`\n    \S+ \(.+:\d+\): get profile`)
	if !traceLine.MatchString(out) {
		t.Errorf("trace line for outer not found:\n%s", out)
	}
}

func TestFormatPlusVOmitsUnsetSections(t *testing.T) {
	e := New(Internal, "boom") // public なし、attrs なし
	out := fmt.Sprintf("%+v", e)
	if strings.Contains(out, "public:") {
		t.Error("public line should be omitted when unset")
	}
	if strings.Contains(out, "attrs:") {
		t.Error("attrs line should be omitted when empty")
	}
	// trace は必ずある(New が 1 フレーム記録するため)。
	mustContain(t, out, "\n  trace:")
	mustContain(t, out, "\n  code: INTERNAL")
}

func mustContain(t *testing.T, s, sub string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Errorf("output missing %q:\n%s", sub, s)
	}
}
