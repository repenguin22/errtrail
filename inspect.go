package errtrail

import (
	"log/slog"
	"net/http"
)

// walk は err から深さ優先で *Error を訪問する。訪問順は「自分 → Unwrap の順」で、
// errors.Join のような Unwrap() []error は先頭ブランチを優先する。fn が false を
// 返した時点で探索を打ち切る(戻り値も false)。
//
// 循環チェーンの検出はしない(標準 errors パッケージ同様、作った側の責任とする)。
func walk(err error, fn func(*Error) bool) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			if !fn(e) {
				return false
			}
		}
		switch x := err.(type) {
		case interface{ Unwrap() error }:
			err = x.Unwrap()
		case interface{ Unwrap() []error }:
			for _, sub := range x.Unwrap() {
				if !walk(sub, fn) {
					return false
				}
			}
			return true
		default:
			return true
		}
	}
	return true
}

// CodeOf は err のチェーンを外側から辿り、最初に見つかった code != OK の *Error の
// Code を返す。err == nil なら OK、*Error が見つからない(または全て code 未設定)
// なら Unknown を返す。
func CodeOf(err error) Code {
	if err == nil {
		return OK
	}
	found := Unknown
	walk(err, func(e *Error) bool {
		if e.code != OK {
			found = e.code
			return false
		}
		return true
	})
	return found
}

// PublicMessage はチェーンを外側から辿り、最初に見つかった非空の public を返す。
// 見つからなければ http.StatusText(CodeOf(err).HTTPStatus()) を返す。
// 内部メッセージには決してフォールバックしない。
func PublicMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := ""
	walk(err, func(e *Error) bool {
		if e.public != "" {
			msg = e.public
			return false
		}
		return true
	})
	if msg != "" {
		return msg
	}
	return http.StatusText(CodeOf(err).HTTPStatus())
}

// Trace はチェーン内の全 *Error のフレームを、外側(最後にラップした場所)から
// 内側(発生源)の順で返す。*Error が無ければ nil。
func Trace(err error) []Frame {
	var frames []Frame
	walk(err, func(e *Error) bool {
		frames = append(frames, resolveFrame(e.pc, e.msg))
		return true
	})
	return frames
}

// Attrs はチェーン内の全 *Error の attrs を、外側から内側の順で連結して返す。
// キーの重複除去はしない(slog 側の挙動に委ねる)。*Error が無ければ nil。
func Attrs(err error) []slog.Attr {
	var attrs []slog.Attr
	walk(err, func(e *Error) bool {
		attrs = append(attrs, e.attrs...)
		return true
	})
	return attrs
}
