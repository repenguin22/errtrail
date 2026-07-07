package errtrail

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Format は fmt.Formatter の実装。
//
//	%s, %v  e.Error() と同一
//	%q      引用符付きの e.Error()
//	%+v     コード・public・attrs・trace を含む複数行形式
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
		// 未知の verb は %v 相当にフォールバックする。
		io.WriteString(s, e.Error())
	}
}

// detailed は %+v の複数行出力を組み立てる。
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

// firstPublic は明示設定された public を外側から探す。フォールバック値は返さない
// (%+v では public が明示されているときだけ行を出す仕様のため)。
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
