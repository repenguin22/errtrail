package errtrail

import (
	"runtime"
	"strconv"
)

// Frame is the caller location recorded by one *Error, together with the
// internal message attached at that point.
type Frame struct {
	Function string // Fully qualified function name, e.g. "example.com/app/repo.(*UserRepo).Get"
	File     string // Full path.
	Line     int
	Msg      string // The internal msg of the *Error that recorded this frame.
}

// String returns "Function (File:Line): Msg". It omits ": Msg" when Msg is
// empty, and " (File:Line)" when both File and Line are zero (an unresolved
// frame, e.g. from a zero-value Error).
func (f Frame) String() string {
	s := f.Function
	if f.File != "" || f.Line != 0 {
		s += " (" + f.File + ":" + strconv.Itoa(f.Line) + ")"
	}
	if f.Msg != "" {
		s += ": " + f.Msg
	}
	return s
}

// caller returns the pc of the user code that called a New/Newf/Wrap/Wrapf
// constructor. Returns 0 on failure. It must be called directly from those
// constructors, since the skip count assumes exactly that call depth.
func caller() uintptr {
	var pcs [1]uintptr
	// Skip 3 frames: runtime.Callers, caller, and the constructor itself,
	// leaving the user's own call site.
	if runtime.Callers(3, pcs[:]) < 1 {
		return 0
	}
	return pcs[0]
}

// resolveFrame resolves a Frame from a pc and msg. When pc is 0, it returns a
// Frame with Function set to "unknown" (a near-zero value rather than nil, so
// callers can handle it uniformly).
func resolveFrame(pc uintptr, msg string) Frame {
	if pc == 0 {
		return Frame{Function: "unknown", Msg: msg}
	}
	frames := runtime.CallersFrames([]uintptr{pc})
	fr, _ := frames.Next()
	return Frame{
		Function: fr.Function,
		File:     fr.File,
		Line:     fr.Line,
		Msg:      msg,
	}
}
