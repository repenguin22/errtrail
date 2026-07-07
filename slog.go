package errtrail

import "log/slog"

// LogValue は slog.LogValuer の実装。エラーをグループ値として表現し、内部メッセージ・
// コード・trace・付帯 attrs を構造化ログに載せる。public はログには含めない
// (public はレスポンス生成専用であり、ログは内部向けのため)。
func (e *Error) LogValue() slog.Value {
	if e == nil {
		return slog.Value{}
	}

	trace := Trace(e)
	traceStrs := make([]string, len(trace))
	for i, f := range trace {
		traceStrs[i] = f.String()
	}

	group := []slog.Attr{
		slog.String("msg", e.Error()),
		slog.String("code", CodeOf(e).String()),
		slog.Any("trace", traceStrs),
	}
	group = append(group, Attrs(e)...)

	return slog.GroupValue(group...)
}
