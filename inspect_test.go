package errtrail

import (
	"errors"
	"fmt"
	"testing"
)

func TestCodeOfNil(t *testing.T) {
	if CodeOf(nil) != OK {
		t.Errorf("CodeOf(nil) = %v, want OK", CodeOf(nil))
	}
}

func TestCodeOfPlainError(t *testing.T) {
	if got := CodeOf(errors.New("x")); got != Unknown {
		t.Errorf("CodeOf(plain) = %v, want Unknown", got)
	}
}

func TestCodeOfWrapDelegatesInner(t *testing.T) {
	inner := New(NotFound, "not found")
	outer := Wrap(inner, "outer") // outer は code 未設定
	if got := CodeOf(outer); got != NotFound {
		t.Errorf("CodeOf = %v, want NotFound", got)
	}
}

func TestCodeOfOuterWins(t *testing.T) {
	inner := New(NotFound, "not found")
	outer := Wrap(inner, "outer").WithCode(Internal)
	if got := CodeOf(outer); got != Internal {
		t.Errorf("CodeOf = %v, want Internal (outer)", got)
	}
}

func TestCodeOfAllUnset(t *testing.T) {
	base := errors.New("x")
	e := Wrap(base, "a") // code 未設定、内側は素の error
	if got := CodeOf(e); got != Unknown {
		t.Errorf("CodeOf = %v, want Unknown", got)
	}
}

func TestPublicMessageFound(t *testing.T) {
	inner := New(NotFound, "internal detail").WithPublic("User not found")
	outer := Wrap(inner, "svc")
	if got := PublicMessage(outer); got != "User not found" {
		t.Errorf("PublicMessage = %q", got)
	}
}

func TestPublicMessageFallback(t *testing.T) {
	e := New(NotFound, "internal detail") // public 未設定
	if got := PublicMessage(e); got != "Not Found" {
		t.Errorf("PublicMessage = %q, want %q (http.StatusText)", got, "Not Found")
	}
}

func TestPublicMessageNeverLeaksInternal(t *testing.T) {
	e := New(Internal, "db password = hunter2")
	got := PublicMessage(e)
	if got == "db password = hunter2" {
		t.Error("PublicMessage leaked internal message")
	}
	if got != "Internal Server Error" {
		t.Errorf("PublicMessage = %q", got)
	}
}

func TestPublicMessageNil(t *testing.T) {
	if PublicMessage(nil) != "" {
		t.Error("PublicMessage(nil) should be empty")
	}
}

func TestTraceOrder(t *testing.T) {
	inner := New(NotFound, "get")   // 発生源
	outer := Wrap(inner, "service") // 外側
	frames := Trace(outer)
	if len(frames) != 2 {
		t.Fatalf("len(frames) = %d, want 2", len(frames))
	}
	// 外側 → 内側の順。
	if frames[0].Msg != "service" {
		t.Errorf("frames[0].Msg = %q, want service", frames[0].Msg)
	}
	if frames[1].Msg != "get" {
		t.Errorf("frames[1].Msg = %q, want get", frames[1].Msg)
	}
}

func TestTraceNoErrorType(t *testing.T) {
	if Trace(errors.New("x")) != nil {
		t.Error("Trace(plain) should be nil")
	}
}

func TestJoinDepthFirst(t *testing.T) {
	a := New(NotFound, "a")
	b := New(Internal, "b")
	joined := errors.Join(a, b)

	// CodeOf は先頭ブランチ優先で最初の non-OK。
	if got := CodeOf(joined); got != NotFound {
		t.Errorf("CodeOf(join) = %v, want NotFound", got)
	}
	// Trace は全件収集。
	if got := len(Trace(joined)); got != 2 {
		t.Errorf("len(Trace(join)) = %d, want 2", got)
	}
}

func TestJoinPublicFirstBranch(t *testing.T) {
	a := New(NotFound, "a") // public 未設定
	b := New(Internal, "b").WithPublic("boom")
	joined := errors.Join(a, b)
	// a に public が無いので探索は b へ進み "boom" を拾う。
	if got := PublicMessage(joined); got != "boom" {
		t.Errorf("PublicMessage(join) = %q, want boom", got)
	}
}

func TestWalkThroughStdFmtChain(t *testing.T) {
	inner := New(NotFound, "inner")
	mid := fmt.Errorf("mid: %w", inner)
	outer := Wrap(mid, "outer")
	if got := CodeOf(outer); got != NotFound {
		t.Errorf("CodeOf through fmt chain = %v, want NotFound", got)
	}
}
