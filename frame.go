package errtrail

import (
	"runtime"
	"strconv"
)

// Frame は 1 つの *Error が記録した呼び出し元の位置と、そのラップに付けた内部メッセージ。
type Frame struct {
	Function string // 完全修飾関数名 例: "example.com/app/repo.(*UserRepo).Get"
	File     string // フルパス
	Line     int
	Msg      string // そのフレームを記録した *Error の内部 msg
}

// String は "Function (File:Line): Msg" を返す。Msg が空なら ": Msg" を省略する。
func (f Frame) String() string {
	s := f.Function + " (" + f.File + ":" + strconv.Itoa(f.Line) + ")"
	if f.Msg != "" {
		s += ": " + f.Msg
	}
	return s
}

// caller は呼び出し元から skip フレーム遡った位置の pc を 1 つ返す。0 は取得失敗。
// New/Wrap から呼ばれ、実際のユーザーコードを指すよう skip を調整する。
func caller(skip int) uintptr {
	var pcs [1]uintptr
	// skip+2: runtime.Callers 自身と caller 自身の 2 フレームを飛ばす。
	if runtime.Callers(skip+2, pcs[:]) < 1 {
		return 0
	}
	return pcs[0]
}

// resolveFrame は pc と msg から Frame を解決する。pc が 0 のときは Function に
// "unknown" を入れて返す(呼び出し側で握れるよう nil ではなくゼロ寄りの値を返す)。
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
