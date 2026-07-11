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
//	%+v     multi-line form including code, public, public.fields,
//	        public.violations, public.retry, attrs, and trace
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

// detailed builds the multi-line %+v output. It gathers code, public, public
// fields, attrs, and trace in a single pass (collect) rather than walking the
// chain once per section. The public line is printed only when a public
// message was explicitly set — collect never yields a fallback value here.
func (e *Error) detailed() string {
	if e == nil {
		return "<nil>"
	}
	c := collect(e, true)
	var b strings.Builder

	b.WriteString(e.Error())
	b.WriteString("\n  code: ")
	b.WriteString(c.code.String())

	if c.public != "" {
		b.WriteString("\n  public: ")
		b.WriteString(c.public)
	}

	// Client-visible extension fields, in walk order with duplicates kept
	// (like attrs); PublicFields' outermost-wins dedup applies only at
	// response generation.
	if len(c.fields) > 0 {
		b.WriteString("\n  public.fields:")
		for _, f := range c.fields {
			b.WriteByte(' ')
			b.WriteString(f.key)
			b.WriteByte('=')
			fmt.Fprint(&b, f.value)
		}
	}

	// Client-visible field violations, matching what FieldViolations returns.
	if len(c.violations) > 0 {
		b.WriteString("\n  public.violations:")
		for _, v := range c.violations {
			b.WriteByte(' ')
			b.WriteString(v.Field)
			b.WriteByte('=')
			b.WriteString(v.Description)
		}
	}

	// Client-visible per-error retry delay, matching LookupRetryDelay.
	if c.retryDelay > 0 {
		b.WriteString("\n  public.retry: ")
		b.WriteString(c.retryDelay.String())
	}

	if len(c.attrs) > 0 {
		b.WriteString("\n  attrs:")
		for _, a := range c.attrs {
			b.WriteByte(' ')
			b.WriteString(a.Key)
			b.WriteByte('=')
			b.WriteString(a.Value.String())
		}
	}

	if len(c.trace) > 0 {
		b.WriteString("\n  trace:")
		for _, f := range c.trace {
			b.WriteString("\n    ")
			b.WriteString(f.String())
		}
	}

	return b.String()
}
