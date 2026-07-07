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

// String returns "Function (File:Line): Msg", omitting ": Msg" when Msg is empty.
func (f Frame) String() string {
	s := f.Function + " (" + f.File + ":" + strconv.Itoa(f.Line) + ")"
	if f.Msg != "" {
		s += ": " + f.Msg
	}
	return s
}

// caller returns the pc of the frame skip levels up from the caller of this
// function. Returns 0 on failure. Called from New/Wrap with skip adjusted so
// it points at the actual user code.
func caller(skip int) uintptr {
	var pcs [1]uintptr
	// skip+2: skip runtime.Callers itself and caller itself.
	if runtime.Callers(skip+2, pcs[:]) < 1 {
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
