package errtrail

import "testing"

func TestFrameString(t *testing.T) {
	tests := []struct {
		name string
		f    Frame
		want string
	}{
		{
			name: "full frame",
			f:    Frame{Function: "example.com/app/repo.Get", File: "/src/repo.go", Line: 42, Msg: "boom"},
			want: "example.com/app/repo.Get (/src/repo.go:42): boom",
		},
		{
			name: "no msg",
			f:    Frame{Function: "example.com/app/repo.Get", File: "/src/repo.go", Line: 42},
			want: "example.com/app/repo.Get (/src/repo.go:42)",
		},
		{
			name: "unresolved with msg",
			f:    Frame{Function: "unknown", Msg: "boom"},
			want: "unknown: boom",
		},
		{
			name: "unresolved without msg",
			f:    Frame{Function: "unknown"},
			want: "unknown",
		},
		{
			// Only File=="" AND Line==0 suppresses the location; a frame
			// with either part set keeps it.
			name: "file without line keeps location",
			f:    Frame{Function: "example.com/app/repo.Get", File: "/src/repo.go"},
			want: "example.com/app/repo.Get (/src/repo.go:0)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.f.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A zero-value Error has pc == 0, so its frame resolves to the "unknown"
// sentinel. Its String must not render a bogus " (:0)" location.
func TestTraceZeroValueError(t *testing.T) {
	frames := Trace(&Error{})
	if len(frames) != 1 {
		t.Fatalf("Trace returned %d frames, want 1", len(frames))
	}
	if got := frames[0].String(); got != "unknown" {
		t.Errorf("String() = %q, want %q", got, "unknown")
	}
}
