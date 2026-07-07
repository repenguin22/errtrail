// Package problem は errtrail のエラーを RFC 9457 (Problem Details for HTTP APIs) の
// JSON レスポンスへ変換する。内部メッセージ・attrs・trace は決して含めず、外部公開
// メッセージ(errtrail.PublicMessage)のみをクライアントに返す。
package problem

import (
	"encoding/json"
	"net/http"

	"github.com/repenguin22/errtrail"
)

// Problem は RFC 9457 の Problem Details オブジェクト。Code は拡張メンバー
// (RFC 9457 §3.2)で、errtrail の Code 名を機械可読な形でクライアントに伝える。
type Problem struct {
	Type   string `json:"type,omitempty"` // 省略時は "about:blank" 相当(RFC 準拠)。
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code"`
}

// TypeURL は Code から type URI を導出するオプショナルなフック。設定する場合は
// サーバ起動前に行うこと(起動後の書き込みは並行読み取りとレースする)。
var TypeURL func(errtrail.Code) string

// From は err から Problem を組み立てる。内部情報は含めない。
//
//	Status = errtrail.CodeOf(err).HTTPStatus()
//	Title  = http.StatusText(Status)
//	Detail = errtrail.PublicMessage(err)(ただし Title と同一なら冗長回避のため空)
//	Code   = errtrail.CodeOf(err).String()
//	Type   = TypeURL が非 nil なら TypeURL(code)、nil なら空
func From(err error) Problem {
	code := errtrail.CodeOf(err)
	status := code.HTTPStatus()
	title := http.StatusText(status)

	detail := errtrail.PublicMessage(err)
	if detail == title {
		detail = ""
	}

	p := Problem{
		Title:  title,
		Status: status,
		Detail: detail,
		Code:   code.String(),
	}
	if TypeURL != nil {
		p.Type = TypeURL(code)
	}
	return p
}

// Write は From(err) を application/problem+json 形式で w に書き込む。
// JSON エンコードに失敗した場合はステータス書き込み後にその error を返す。
func Write(w http.ResponseWriter, err error) error {
	p := From(err)
	body, mErr := json.Marshal(p)
	if mErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return mErr
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_, wErr := w.Write(body)
	return wErr
}
