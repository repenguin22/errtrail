# errtrail — Design Spec

Web サービス(HTTP / gRPC)向けの Go エラーライブラリ。

- Status: Draft v1
- Module: `github.com/repenguin22/errtrail`(仮。公開先が決まったら変更)
- Go: 1.22+(`log/slog` に依存するため 1.21 が下限。開発は 1.22 を前提とする)

---

## 1. ゴールと非ゴール

### ゴール

1. **エラーコードを一次情報とする。** コードは gRPC の 16 種(`codes.Code`)に準拠した独自型 `Code` で表し、HTTP ステータス / gRPC ステータスへは変換テーブルで導出する。
2. **コアは標準ライブラリのみに依存する。** gRPC の `*status.Status` への変換だけは別 go.mod のサブモジュール `grpcerr` に隔離する。
3. **発生箇所と伝播パスの追跡。** `New` / `Wrap` のたびに呼び出し元 1 フレームだけを記録する。フルスタックトレースは取らない。
4. **標準 `errors` パッケージと完全互換。** `errors.Is` / `errors.As` / `errors.Unwrap` / `errors.Join` と共存できる。
5. **内部メッセージと外部公開メッセージの分離。** クライアントに返るのは明示的に設定した public message のみ。
6. **構造化ログ連携。** `slog.LogValuer` を実装する。
7. **RFC 9457 (Problem Details) での HTTP レスポンス生成**をサブパッケージ `problem` で提供する。

### 非ゴール(v1 スコープ外)

- リトライ可否フラグ、ログレベルヒント等のメタデータ
- gRPC の `errdetails`(BadRequest 等のリッチ詳細)
- HTTP ステータス → `Code` の逆変換
- gRPC インターセプタ、HTTP ミドルウェア
- 多言語化(i18n)

---

## 2. パッケージ構成

```
errtrail/
├── go.mod                 // module github.com/repenguin22/errtrail(依存: 標準ライブラリのみ)
├── code.go                // Code 型、定数、Register、HTTP/gRPC マッピング
├── error.go               // Error 型、New/Newf/Wrap/Wrapf、ビルダーメソッド
├── frame.go               // Frame 型、pc の記録と遅延解決
├── inspect.go             // CodeOf, PublicMessage, Trace, Attrs
├── format.go              // fmt.Formatter 実装
├── slog.go                // slog.LogValuer 実装
├── problem/
│   └── problem.go         // RFC 9457(依存: 標準ライブラリのみ)
└── grpcerr/
    ├── go.mod             // module github.com/repenguin22/errtrail/grpcerr(依存: google.golang.org/grpc)
    └── grpcerr.go
```

`problem` は標準ライブラリのみで書けるためコアと同一モジュール。`grpcerr` のみ別モジュール。

---

## 3. `Code` 型

```go
// Code はエラーの分類。値 0–16 は gRPC の codes.Code と同一の意味・数値を持つ。
type Code uint32

const (
    OK                 Code = 0  // エラーなし。Error に設定することは想定しない
    Canceled           Code = 1
    Unknown            Code = 2
    InvalidArgument    Code = 3
    DeadlineExceeded   Code = 4
    NotFound           Code = 5
    AlreadyExists      Code = 6
    PermissionDenied   Code = 7
    ResourceExhausted  Code = 8
    FailedPrecondition Code = 9
    Aborted            Code = 10
    OutOfRange         Code = 11
    Unimplemented      Code = 12
    Internal           Code = 13
    Unavailable        Code = 14
    DataLoss           Code = 15
    Unauthenticated    Code = 16
)
```

### 3.1 メソッド

```go
// String はコード名を返す。組み込みは "NOT_FOUND" 形式(gRPC 準拠の SCREAMING_SNAKE)。
// 未登録のカスタムコードは "CODE(123)" 形式。
func (c Code) String() string

// HTTPStatus は対応する HTTP ステータスコードを返す。未登録コードは 500。
func (c Code) HTTPStatus() int

// GRPCCode は対応する gRPC コードの数値を返す。0–16 は恒等、
// カスタムコードは Register で指定された値、未登録は 2 (UNKNOWN)。
// grpc パッケージに依存しないよう uint32 で返す。
func (c Code) GRPCCode() uint32
```

### 3.2 HTTP マッピングテーブル(組み込み分)

gRPC 公式の HTTP マッピング(grpc-gateway 準拠)をそのまま採用する。

| Code | 名前 | HTTP |
|---|---|---|
| 0 | OK | 200 |
| 1 | CANCELED | 499 |
| 2 | UNKNOWN | 500 |
| 3 | INVALID_ARGUMENT | 400 |
| 4 | DEADLINE_EXCEEDED | 504 |
| 5 | NOT_FOUND | 404 |
| 6 | ALREADY_EXISTS | 409 |
| 7 | PERMISSION_DENIED | 403 |
| 8 | RESOURCE_EXHAUSTED | 429 |
| 9 | FAILED_PRECONDITION | 400 |
| 10 | ABORTED | 409 |
| 11 | OUT_OF_RANGE | 400 |
| 12 | UNIMPLEMENTED | 501 |
| 13 | INTERNAL | 500 |
| 14 | UNAVAILABLE | 503 |
| 15 | DATA_LOSS | 500 |
| 16 | UNAUTHENTICATED | 401 |

### 3.3 カスタムコードの登録

```go
// Register はカスタムコードを登録する。init 時(サービス起動前)に呼ぶことを想定し、
// 登録処理自体は並行安全にしない(内部 map への素の書き込み。読み取りは起動後のみと規定)。
// c < 100、または登録済みコードとの重複は panic。
func Register(c Code, name string, httpStatus int, grpcCode uint32)
```

- 0–99 は予約(現状 0–16 のみ使用、将来の組み込み追加用に確保)。カスタムは 100 以上。
- 実装は `map[Code]codeInfo`(`codeInfo{name string; httpStatus int; grpcCode uint32}`)のパッケージ変数。組み込み 17 種も同じ map に初期投入し、lookup を一本化する。
- doc comment に「`Register` は `init` またはサーバ起動前に呼ぶこと。起動後の呼び出しはデータレース」と明記する。

---

## 4. `Error` 型

```go
type Error struct {
    code   Code        // ゼロ値 OK は「未設定」を意味し、CodeOf では内側の Error に委譲する
    msg    string      // 内部メッセージ(ログ用)。クライアントには決して出さない
    public string      // 外部公開メッセージ。空なら未設定
    cause  error       // ラップした元エラー。nil 可
    pc     uintptr     // 記録した呼び出し元 1 フレーム(遅延解決)。0 は「なし」
    attrs  []slog.Attr // 構造化ログ用の属性
}
```

**イミュータブル**: 生成後にフィールドを変更する API は提供しない。`With*` 系はすべてシャローコピーを返す。したがって並行アクセスに対して安全。

### 4.1 コンストラクタ

```go
// New は新しいエラーを作る。呼び出し元 1 フレームを記録する。
func New(code Code, msg string) *Error

// Newf は fmt.Sprintf 形式の New。%w は使えない(ラップは Wrap を使う)。
func Newf(code Code, format string, args ...any) *Error

// Wrap は err をラップし、呼び出し元 1 フレームを記録する。
// code は未設定(OK)のままにし、CodeOf はチェーン内側の Code に委譲する。
// コードを付け替えたい場合は Wrap(...).WithCode(c) を使う。
// err == nil のとき nil を返す(if err != nil を省いた呼び出しを安全にする)。
func Wrap(err error, msg string) *Error

// Wrapf は fmt.Sprintf 形式の Wrap。同じく err == nil なら nil。
func Wrapf(err error, format string, args ...any) *Error
```

フレーム記録は `runtime.Callers(2, pc[:1])` で pc を 1 つだけ取得して保持する。`file:line` への解決は表示時まで遅延する(`runtime.CallersFrames`)。これにより生成コストは数十 ns・アロケーション最小に収まる。

### 4.2 ビルダーメソッド

すべてレシーバのシャローコピーを返す。**フレームは再記録しない**(記録は New/Wrap の責務)。attrs の追加は「コピーして append」で行い、元スライスとの共有を避ける(`append(slices.Clip(e.attrs), ...)` 相当)。

```go
// WithCode はコードを差し替えたコピーを返す。
func (e *Error) WithCode(c Code) *Error

// WithPublic は外部公開メッセージを設定したコピーを返す。
func (e *Error) WithPublic(msg string) *Error

// With は slog.Attr を追加したコピーを返す。
// 例: e.With(slog.String("user_id", id), slog.Int("attempt", n))
func (e *Error) With(attrs ...slog.Attr) *Error
```

全ビルダーメソッドと後述のアクセサは **nil レシーバ安全**(nil なら nil / ゼロ値を返す)。`Wrap` が nil を返しうるため、チェーン呼び出しを panic させないための規定。

### 4.3 error インターフェースと Unwrap

```go
// Error は "msg: cause" を返す。cause == nil なら msg のみ。
// msg が空で cause がある場合は cause.Error() のみ(コロンを残さない)。
func (e *Error) Error() string

func (e *Error) Unwrap() error // cause を返す
```

`Is` / `As` は独自実装しない。標準の `errors.Is/As` が `Unwrap` を辿ることで機能する。コードによる比較は `CodeOf` で明示的に行う設計とし、「`errors.Is(err, errtrail.NotFound)` 的な暗黙マッチ」は提供しない(挙動が推測しづらいため)。

---

## 5. 検査 API(inspect.go)

```go
// CodeOf は err のチェーンを外側から辿り、最初に見つかった
// code != OK の *Error の Code を返す。
// err == nil なら OK、*Error が見つからない(または全部 code 未設定)なら Unknown。
// 探索は Unwrap() error と Unwrap() []error(errors.Join)の両方を深さ優先で辿る。
func CodeOf(err error) Code

// PublicMessage はチェーンを外側から辿り、最初に見つかった非空の public を返す。
// 見つからなければ http.StatusText(CodeOf(err).HTTPStatus()) を返す
// (例: NotFound → "Not Found")。内部メッセージには決してフォールバックしない。
func PublicMessage(err error) string

// Trace はチェーン内の全 *Error のフレームを外側(最後にラップした場所)から
// 内側(発生源)の順で返す。*Error が無ければ nil。
func Trace(err error) []Frame

// Attrs はチェーン内の全 *Error の attrs を外側から内側の順で連結して返す。
// キーの重複除去はしない(slog 側の挙動に委ねる)。
func Attrs(err error) []slog.Attr
```

### 5.1 Frame

```go
type Frame struct {
    Function string // 完全修飾関数名 例: "example.com/app/repo.(*UserRepo).Get"
    File     string // フルパス
    Line     int
    Msg      string // そのフレームを記録した *Error の内部 msg
}

// String は "Function (File:Line): Msg" を返す。Msg が空なら ": Msg" を省略。
func (f Frame) String() string
```

### 5.2 チェーン探索の共通実装

`CodeOf` / `PublicMessage` / `Trace` / `Attrs` は同じ walk 関数を共有する:

```go
// walk は err から深さ優先(自分 → Unwrap の順、Join は先頭ブランチ優先)で
// *Error を訪問する。fn が false を返したら打ち切り。
func walk(err error, fn func(*Error) bool)
```

- `errors.Join` / `Unwrap() []error` 実装に遭遇したら各要素を順に再帰する。
- 循環対策は不要(標準 errors パッケージ同様、循環チェーンは作った側の責任とする)。ただし再帰深度は Go のスタックに任せる。

---

## 6. fmt.Formatter(format.go)

```go
func (e *Error) Format(s fmt.State, verb rune)
```

| verb | 出力 |
|---|---|
| `%s`, `%v` | `e.Error()` と同一 |
| `%q` | `strconv.Quote(e.Error())` |
| `%+v` | 下記の複数行形式 |

`%+v` の出力形式(正確にこの通りに実装する):

```
get profile: query user: sql: no rows in result set
  code: NOT_FOUND
  public: User not found
  attrs: user_id=42 attempt=3
  trace:
    example.com/app/service.(*UserService).Profile (/src/app/service/user.go:88): get profile
    example.com/app/repo.(*UserRepo).Get (/src/app/repo/user.go:42): query user
```

- 1 行目: `e.Error()`(チェーン全体の連結メッセージ)
- `code:` 行: `CodeOf(e).String()`
- `public:` 行: 明示設定された public があるときのみ出力(フォールバック値は出さない)
- `attrs:` 行: `Attrs(e)` が空なら行ごと省略。`key=value` を半角スペース区切り
- `trace:` 以下: `Trace(e)` の各 Frame を `Frame.String()` で 1 行ずつ。空なら `trace:` ごと省略
- インデントは半角スペース 2 個、trace の要素は 4 個

---

## 7. slog 連携(slog.go)

```go
// LogValue は slog.LogValuer の実装。グループ値を返す。
func (e *Error) LogValue() slog.Value
```

返すグループの内容:

| key | 型 | 値 |
|---|---|---|
| `msg` | string | `e.Error()` |
| `code` | string | `CodeOf(e).String()` |
| `trace` | []string | `Trace(e)` の各要素の `Frame.String()` |
| (attrs) | — | `Attrs(e)` をグループ直下に展開 |

`public` はログには含めない(ログは内部向けであり、public はレスポンス生成専用)。

使用例:

```go
slog.Error("request failed", slog.Any("error", err))
// JSON: {"msg":"request failed","error":{"msg":"get profile: ...","code":"NOT_FOUND",
//        "trace":["...(user.go:88): get profile","..."],"user_id":42}}
```

注意: `slog.Any("error", err)` の err が `*Error` 以外(素の error)の場合は slog 標準の挙動になる。ドキュメントで「`errtrail.Wrap` してから渡す」ことを推奨として書く。

---

## 8. problem パッケージ(RFC 9457)

`github.com/repenguin22/errtrail/problem`。依存は `encoding/json`, `net/http`, コアの `errtrail` のみ。

```go
// Problem は RFC 9457 の Problem Details オブジェクト。
// Code は拡張メンバー(RFC 9457 §3.2)で、errtrail の Code 名を機械可読に伝える。
type Problem struct {
    Type   string `json:"type,omitempty"`   // 省略時は "about:blank" 扱い(RFC 準拠)
    Title  string `json:"title"`
    Status int    `json:"status"`
    Detail string `json:"detail,omitempty"`
    Code   string `json:"code"`
}

// From は err から Problem を組み立てる。
//   Status = errtrail.CodeOf(err).HTTPStatus()
//   Title  = http.StatusText(Status)
//   Detail = errtrail.PublicMessage(err) — ただし Title と同一値なら空にする(冗長回避)
//   Code   = errtrail.CodeOf(err).String()
//   Type   = TypeURL が非 nil なら TypeURL(CodeOf(err))、nil なら空
// 内部メッセージ・attrs・trace は決して含めない。
func From(err error) Problem

// TypeURL は Code から type URI を導出するオプショナルなフック。
// パッケージ変数。設定する場合はサーバ起動前に行うこと(並行書き込み不可)。
var TypeURL func(errtrail.Code) string

// Write は From(err) を application/problem+json で w に書き込む。
//   - Content-Type: application/problem+json ヘッダを設定
//   - WriteHeader(p.Status)
//   - json.Encode 失敗時は握りつぶさず error で返す
func Write(w http.ResponseWriter, err error) error
```

使用例:

```go
func handler(w http.ResponseWriter, r *http.Request) {
    user, err := svc.Profile(r.Context(), id)
    if err != nil {
        slog.ErrorContext(r.Context(), "profile failed", slog.Any("error", err))
        _ = problem.Write(w, err)
        return
    }
    ...
}
```

---

## 9. grpcerr パッケージ(別モジュール)

`github.com/repenguin22/errtrail/grpcerr`。独自 go.mod を持ち、`google.golang.org/grpc` に依存する。コアの `errtrail` には replace なしで依存できるよう、コアを先にタグ付けしてから grpcerr をタグ付けする運用(`grpcerr/vX.Y.Z` 形式のタグ)。

```go
// ToStatus は err を *status.Status に変換する。
//   code    = codes.Code(errtrail.CodeOf(err).GRPCCode())
//   message = errtrail.PublicMessage(err)
// err == nil のときは status.New(codes.OK, "")。
func ToStatus(err error) *status.Status

// ToError は ToStatus(err).Err() を返す。gRPC ハンドラの return 用。
// err == nil なら nil。
func ToError(err error) error
```

使用例:

```go
func (s *server) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.User, error) {
    u, err := s.svc.Get(ctx, req.Id)
    if err != nil {
        slog.ErrorContext(ctx, "get user failed", slog.Any("error", err))
        return nil, grpcerr.ToError(err)
    }
    ...
}
```

インターセプタは v1 では提供しない(各サービスのロギング方針と密結合になるため)。

---

## 10. エッジケース仕様(まとめ)

| ケース | 挙動 |
|---|---|
| `Wrap(nil, ...)` / `Wrapf(nil, ...)` | `nil` を返す |
| nil レシーバへのメソッド呼び出し | `With*` は nil を返す。`Error()` は `"<nil>"`。`Unwrap`/`LogValue` 等はゼロ値 |
| `CodeOf(nil)` | `OK` |
| `CodeOf(fmt.Errorf("x"))`(*Error なし) | `Unknown` |
| `Wrap` だけでコード未設定のチェーン | 内側の `*Error` のコードに委譲。どこにもなければ `Unknown` |
| `PublicMessage` で public 未設定 | `http.StatusText(HTTPStatus)` にフォールバック。内部 msg は使わない |
| `errors.Join(a, b)` の両方が `*Error` | 深さ優先で先勝ち(`CodeOf`/`PublicMessage` は最初の 1 件、`Trace`/`Attrs` は全件収集) |
| カスタムコード未登録のまま使用 | `String()` は `"CODE(n)"`、HTTP 500、gRPC UNKNOWN(2) |
| `Register(c < 100, ...)` / 重複登録 | panic |
| `New(OK, ...)` | 禁止しない(vet 不能)が、doc で非推奨と明記。`CodeOf` は code==OK の Error をスキップするため実質 Unknown 扱いになる |

---

## 11. テスト計画

- **code_test.go**: 全 17 組み込みコードのマッピング(HTTP/gRPC/String)をテーブルテスト。Register の正常系・panic 系。
- **error_test.go**: New/Wrap/ビルダーのイミュータブル性(元の Error が変化しないこと、attrs スライスの共有がないこと)。nil 安全性の全パターン。`errors.Is/As/Unwrap` 互換(標準 `%w` チェーンとの混在含む)。
- **inspect_test.go**: 上記エッジケース表を網羅。`errors.Join` を含む探索順。
- **format_test.go**: `%s` / `%v` / `%q` / `%+v` の出力をゴールデン文字列で比較(file/line は正規表現でマッチ)。
- **slog_test.go**: `slog.NewJSONHandler` + バッファで実際の JSON 出力を検証。
- **problem_test.go**: `httptest.ResponseRecorder` で Content-Type / status / body を検証。Detail==Title のときの省略。
- **grpcerr/grpcerr_test.go**: status 変換。カスタムコードの gRPC マッピング。
- **ベンチマーク**(bench_test.go): `New`, `Wrap`, `Wrap x3 のチェーン生成`, `%+v` フォーマット。`New` は 1 alloc 程度を目標とし、リグレッション検知のため `-benchmem` の結果を README に記録。

## 12. 実装順序

1. `code.go`(+ テスト)— 依存なしで独立
2. `frame.go` → `error.go`(+ テスト)
3. `inspect.go`(walk 実装)(+ テスト)
4. `format.go`, `slog.go`(+ テスト)
5. `problem/`(+ テスト)
6. コアを v0.1.0 タグ → `grpcerr/`(+ テスト)→ `grpcerr/v0.1.0` タグ
