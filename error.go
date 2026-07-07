package errtrail

import (
	"fmt"
	"log/slog"
	"slices"
)

// Error は errtrail の中核となるエラー型。イミュータブルであり、生成後にフィールドを
// 変更する API は持たない。With 系メソッドはシャローコピーを返すため、複数の
// goroutine から同じ *Error を安全に共有・派生できる。
type Error struct {
	code   Code        // ゼロ値 OK は「未設定」。CodeOf は内側の Error に委譲する。
	msg    string      // 内部メッセージ(ログ用)。クライアントには決して出さない。
	public string      // 外部公開メッセージ。空なら未設定。
	cause  error       // ラップした元エラー。nil 可。
	pc     uintptr     // 記録した呼び出し元 1 フレーム(遅延解決)。0 は「なし」。
	attrs  []slog.Attr // 構造化ログ用の属性。
}

// New は新しいエラーを作る。呼び出し元 1 フレームを記録する。
func New(code Code, msg string) *Error {
	return &Error{code: code, msg: msg, pc: caller(1)}
}

// Newf は fmt.Sprintf 形式の New。%w は使えない(ラップは Wrap を使う)。
func Newf(code Code, format string, args ...any) *Error {
	return &Error{code: code, msg: fmt.Sprintf(format, args...), pc: caller(1)}
}

// Wrap は err をラップし、呼び出し元 1 フレームを記録する。code は未設定(OK)の
// ままにし、CodeOf はチェーン内側の Code に委譲する。コードを付け替えたい場合は
// Wrap(...).WithCode(c) を使う。
//
// err == nil のときは nil を返す。これにより if err != nil を省いた呼び出しが安全になる。
func Wrap(err error, msg string) *Error {
	if err == nil {
		return nil
	}
	return &Error{msg: msg, cause: err, pc: caller(1)}
}

// Wrapf は fmt.Sprintf 形式の Wrap。err == nil なら nil を返す。
func Wrapf(err error, format string, args ...any) *Error {
	if err == nil {
		return nil
	}
	return &Error{msg: fmt.Sprintf(format, args...), cause: err, pc: caller(1)}
}

// WithCode はコードを差し替えたコピーを返す。フレームは再記録しない。
func (e *Error) WithCode(c Code) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.code = c
	return &cp
}

// WithPublic は外部公開メッセージを設定したコピーを返す。フレームは再記録しない。
func (e *Error) WithPublic(msg string) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	cp.public = msg
	return &cp
}

// With は slog.Attr を追加したコピーを返す。フレームは再記録しない。
//
// 例: e.With(slog.String("user_id", id), slog.Int("attempt", n))
func (e *Error) With(attrs ...slog.Attr) *Error {
	if e == nil {
		return nil
	}
	cp := *e
	// 元スライスと基底配列を共有しないよう Clip してから append する。
	cp.attrs = append(slices.Clip(e.attrs), attrs...)
	return &cp
}

// Error は "msg: cause" を返す。cause が nil なら msg のみ。msg が空で cause が
// あるときは cause.Error() のみ(余分なコロンを残さない)。
func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	switch {
	case e.cause == nil:
		return e.msg
	case e.msg == "":
		return e.cause.Error()
	default:
		return e.msg + ": " + e.cause.Error()
	}
}

// Unwrap はラップした元エラーを返す。標準の errors.Is / errors.As がチェーンを
// 辿るために使う。
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}
