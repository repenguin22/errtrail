package errtrail

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Format implements fmt.Formatter.
//
//	%s, %v  same as e.Error()
//	%q      quoted e.Error()
//	%+v     multi-line form including code, public, attrs, and trace
func (e *Error) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		if s.Flag('+') {
			io.WriteString(s, e.detailed())
			return
		}
		io.WriteString(s, e.Error())
	case 's':
		io.WriteString(s, e.Error())
	case 'q':
		io.WriteString(s, strconv.Quote(e.Error()))
	default:
		// Unknown verbs fall back to the %v form.
		io.WriteString(s, e.Error())
	}
}

// detailed builds the multi-line %+v output.
func (e *Error) detailed() string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder

	b.WriteString(e.Error())
	b.WriteString("\n  code: ")
	b.WriteString(CodeOf(e).String())

	if pub := firstPublic(e); pub != "" {
		b.WriteString("\n  public: ")
		b.WriteString(pub)
	}

	if attrs := Attrs(e); len(attrs) > 0 {
		b.WriteString("\n  attrs:")
		for _, a := range attrs {
			b.WriteByte(' ')
			b.WriteString(a.Key)
			b.WriteByte('=')
			b.WriteString(a.Value.String())
		}
	}

	if trace := Trace(e); len(trace) > 0 {
		b.WriteString("\n  trace:")
		for _, f := range trace {
			b.WriteString("\n    ")
			b.WriteString(f.String())
		}
	}

	return b.String()
}

// firstPublic looks for an explicitly-set public message, outermost first.
// It never returns a fallback value — %+v only prints the public line when
// one was explicitly set.
func firstPublic(err error) string {
	msg := ""
	walk(err, func(e *Error) bool {
		if e.public != "" {
			msg = e.public
			return false
		}
		return true
	})
	return msg
}
