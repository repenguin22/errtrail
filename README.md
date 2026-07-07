# errtrail

[![CI](https://github.com/repenguin22/errtrail/actions/workflows/ci.yml/badge.svg)](https://github.com/repenguin22/errtrail/actions/workflows/ci.yml)

Web サービス(HTTP / gRPC)向けの Go エラーライブラリ。設計の詳細は [DESIGN.md](DESIGN.md)。

- **エラーコードが一次情報** — `Code` の 0–16 は gRPC の `codes.Code` と同一。HTTP / gRPC ステータスは変換テーブルで導出。
- **コアは標準ライブラリのみ** — gRPC 変換だけ別モジュール `grpcerr` に隔離。
- **軽量な発生箇所追跡** — `New` / `Wrap` ごとに呼び出し元 1 フレームを記録。スタックトレースは取らない。
- **標準 `errors` 完全互換** — `Is` / `As` / `Unwrap` / `Join` と共存。
- **内部 / 外部メッセージの分離** — クライアントには public message のみ。
- **`slog.LogValuer` 実装** と **RFC 9457 (Problem Details)** レスポンス。

## インストール

```
go get github.com/repenguin22/errtrail
go get github.com/repenguin22/errtrail/grpcerr   # gRPC を使う場合のみ
```

## 使い方

```go
// 発生源: コードと内部メッセージ、公開メッセージ、属性を付ける。
err := errtrail.New(errtrail.NotFound, "user row missing").
    WithPublic("User not found").
    With(slog.String("user_id", id))

// 中間層: 文脈を足してラップ。コードは内側から引き継がれる。
if err != nil {
    return errtrail.Wrap(err, "load profile")
}
```

境界での取り回し:

```go
// HTTP ハンドラ
slog.ErrorContext(ctx, "request failed", slog.Any("error", err)) // 内部情報を全部ログ
_ = problem.Write(w, err)                                        // クライアントには public のみ

// gRPC ハンドラ
return nil, grpcerr.ToError(err)
```

`%+v` で伝播パスを確認:

```
load profile: query user: sql: no rows in result set
  code: NOT_FOUND
  public: User not found
  attrs: user_id=42
  trace:
    example.com/app/service.(*UserService).Profile (/src/app/service/user.go:88): load profile
    example.com/app/repo.(*UserRepo).Get (/src/app/repo/user.go:42): query user
```

## パッケージ

| パッケージ | 依存 | 役割 |
|---|---|---|
| `errtrail` | 標準ライブラリのみ | コア。`Code`, `Error`, 検査・整形・slog |
| `errtrail/problem` | 標準ライブラリのみ | RFC 9457 レスポンス生成 |
| `errtrail/grpcerr` | `google.golang.org/grpc` | `*status.Status` 変換(別 go.mod) |

## カスタムコード

100 以上を `init` またはサーバ起動前に登録する(起動後の登録は非対応)。

```go
const RateLimited errtrail.Code = 100

func init() {
    errtrail.Register(RateLimited, "RATE_LIMITED", http.StatusTooManyRequests, 8 /* ResourceExhausted */)
}
```

## ベンチマーク

Apple M-series, Go 1.26。`New` / `Wrap` はフレーム記録込みで 1 alloc。

```
BenchmarkNew-10          8312416    141.2 ns/op    96 B/op   1 allocs/op
BenchmarkWrap-10         8593356    148.6 ns/op    96 B/op   1 allocs/op
BenchmarkWrapChain3-10   2671467    441.1 ns/op   288 B/op   3 allocs/op
BenchmarkFormatPlusV-10   869619   1329   ns/op  3345 B/op  24 allocs/op
```

## リリース手順

コアと `grpcerr` は別モジュールなので独立にタグ付けする。

```
git tag v0.1.0            # コア
git tag grpcerr/v0.1.0    # grpcerr サブモジュール
```

`grpcerr` の `require` はタグ付けされたコアのバージョンを参照する(replace なし)。コアと grpcerr を同時に変更する開発時は、リポジトリ外に `go.work` を置いて両モジュールを use するとローカル参照になる。
