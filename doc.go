// Package errtrail は Web サービス(HTTP / gRPC)向けの Go エラーライブラリである。
//
// 設計の柱:
//
//   - エラーコード(Code)を一次情報とし、HTTP / gRPC ステータスは変換テーブルで
//     導出する。Code の 0–16 は gRPC の codes.Code と同一の意味・数値を持つ。
//   - コアは標準ライブラリのみに依存する。gRPC 変換は別モジュール grpcerr に隔離。
//   - New / Wrap のたびに呼び出し元 1 フレームだけを記録し、伝播パスを追跡できる。
//     フルスタックトレースは取らない(生成コストは約 100–150ns / 1 alloc)。
//   - 標準 errors パッケージ(Is / As / Unwrap / Join)と完全互換。
//   - 内部メッセージ(ログ用)と外部公開メッセージ(クライアント返却用)を分離する。
//   - slog.LogValuer を実装し、構造化ログにコード・trace・属性を載せる。
//
// 基本的な流れ:
//
//	// 発生源: コードと内部メッセージを付ける。
//	if row == nil {
//	    return errtrail.New(errtrail.NotFound, "user row missing").
//	        WithPublic("User not found").
//	        With(slog.String("user_id", id))
//	}
//
//	// 中間層: 文脈を足してラップする。コードは内側から引き継がれる。
//	if err != nil {
//	    return errtrail.Wrap(err, "load profile")
//	}
//
//	// 境界(HTTP ハンドラ): ログには全情報、クライアントには public のみ。
//	slog.ErrorContext(ctx, "request failed", slog.Any("error", err))
//	_ = problem.Write(w, err) // RFC 9457 レスポンス
//
// HTTP レスポンスは problem サブパッケージ、gRPC 変換は grpcerr サブモジュールを使う。
package errtrail
