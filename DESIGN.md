# errtrail — Design Spec

A Go error library for web services (HTTP / gRPC).

- Status: Draft v1
- Module: `github.com/repenguin22/errtrail`
- Go: 1.22+ (1.21 is the floor imposed by `log/slog`; development targets 1.22)

---

## 1. Goals and non-goals

### Goals

1. **Error codes are the source of truth.** Codes are represented by a custom `Code` type aligned with gRPC's 16 codes (`codes.Code`); HTTP and gRPC statuses are derived from it via a lookup table.
2. **The core depends on the standard library only.** Only the conversion to gRPC's `*status.Status` is isolated in a separate go.mod submodule, `grpcerr`.
3. **Track the origin and propagation path of an error.** `New` / `Wrap` each record just one caller frame. No full stack traces.
4. **Fully compatible with the standard `errors` package.** Works alongside `errors.Is` / `errors.As` / `errors.Unwrap` / `errors.Join`.
5. **Separate internal messages from public messages.** Only an explicitly-set public message is ever returned to a client.
6. **Structured logging integration.** Implements `slog.LogValuer`.
7. **RFC 9457 (Problem Details) HTTP response generation**, provided via the `problem` subpackage.

### Non-goals (out of scope for v1)

- Metadata such as a log-level hint — a retryable flag is supported as of v0.3.0 (`IsRetryable` / `Register`'s `Retryable` option, §3.3)
- gRPC's rich `errdetails` beyond `ErrorInfo` (BadRequest, RetryInfo, etc.) — an opt-in `ErrorInfo` carrying the code name is supported via `grpcerr.Domain` (§9)
- Reverse conversion from HTTP status back to `Code`
- gRPC interceptors, HTTP middleware
- Internationalization (i18n)

---

## 2. Package layout

```
errtrail/
├── go.mod                 // module github.com/repenguin22/errtrail (deps: stdlib only)
├── code.go                // Code type, constants, Register, HTTP/gRPC mapping
├── error.go               // Error type, New/Newf/Wrap/Wrapf, builder methods
├── frame.go                // Frame type, pc recording and lazy resolution
├── inspect.go             // CodeOf, PublicMessage, Trace, Attrs
├── format.go              // fmt.Formatter implementation
├── slog.go                // slog.LogValuer implementation
├── problem/
│   └── problem.go         // RFC 9457 (deps: stdlib only)
└── grpcerr/
    ├── go.mod             // module github.com/repenguin22/errtrail/grpcerr (deps: google.golang.org/grpc)
    └── grpcerr.go
```

`problem` can be written using only the standard library, so it lives in the core module. Only `grpcerr` is a separate module.

---

## 3. The `Code` type

```go
// Code classifies an error. Values 0–16 share the same meaning and numeric
// value as gRPC's codes.Code.
type Code uint32

const (
    OK                 Code = 0  // No error. Not expected to be set on an Error.
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

### 3.1 Methods

```go
// String returns the code's name — built-ins in "NOT_FOUND" form (gRPC's
// SCREAMING_SNAKE convention). An unregistered custom code returns
// "CODE(123)".
func (c Code) String() string

// HTTPStatus returns the corresponding HTTP status code. Unregistered codes
// return 500.
func (c Code) HTTPStatus() int

// GRPCCode returns the corresponding gRPC code as a number. 0–16 map to
// themselves, custom codes return the value passed to Register, and
// unregistered codes return 2 (UNKNOWN). Returned as uint32 so this package
// need not depend on the grpc package.
func (c Code) GRPCCode() uint32

// Retryable reports whether the code classifies a failure worth retrying.
// Built-ins: DeadlineExceeded, ResourceExhausted, Aborted, and Unavailable
// return true; everything else false. Custom codes return true only if
// registered with the Retryable option; unregistered codes return false.
func (c Code) Retryable() bool
```

### 3.2 HTTP mapping table (built-ins)

Adopts gRPC's official HTTP mapping as-is (aligned with grpc-gateway). The Retryable column reflects the built-in retryability set: DEADLINE_EXCEEDED (may succeed under a fresh deadline), RESOURCE_EXHAUSTED (after backoff), ABORTED (transaction-style retry), UNAVAILABLE (the canonical transient failure). CANCELED is not retryable — the caller gave up deliberately — and UNKNOWN is conservatively not.

| Code | Name | HTTP | Retryable |
|---|---|---|---|
| 0 | OK | 200 | |
| 1 | CANCELED | 499 | |
| 2 | UNKNOWN | 500 | |
| 3 | INVALID_ARGUMENT | 400 | |
| 4 | DEADLINE_EXCEEDED | 504 | ✔ |
| 5 | NOT_FOUND | 404 | |
| 6 | ALREADY_EXISTS | 409 | |
| 7 | PERMISSION_DENIED | 403 | |
| 8 | RESOURCE_EXHAUSTED | 429 | ✔ |
| 9 | FAILED_PRECONDITION | 400 | |
| 10 | ABORTED | 409 | ✔ |
| 11 | OUT_OF_RANGE | 400 | |
| 12 | UNIMPLEMENTED | 501 | |
| 13 | INTERNAL | 500 | |
| 14 | UNAVAILABLE | 503 | ✔ |
| 15 | DATA_LOSS | 500 | |
| 16 | UNAUTHENTICATED | 401 | |

### 3.3 Registering custom codes

```go
// RegisterOption customizes a code being registered.
type RegisterOption func(*codeInfo)

// Retryable returns a RegisterOption marking the code as retryable
// (IsRetryable / (Code).Retryable report true). Not retryable by default.
func Retryable() RegisterOption

// Register adds a custom code. Intended to be called at init time (before
// the service starts); registration itself is not made concurrency-safe
// (a plain write to an internal map — reads are specified to only happen
// after startup).
// Panics if c < 100, if the code is already registered, if name is empty,
// if httpStatus is outside [100, 599], or if grpcCode is above 16.
func Register(c Code, name string, httpStatus int, grpcCode uint32, opts ...RegisterOption)
```

- 0–99 are reserved (currently only 0–16 are used; the rest is held for future built-ins). Custom codes start at 100.
- Implemented as a package-level `map[Code]codeInfo` (`codeInfo{name string; httpStatus int; grpcCode uint32; retryable bool}`). The 17 built-ins are seeded into the same map at init, so lookups go through a single path.
- The doc comment states explicitly: "call `Register` from `init` or before the server starts. Calling it afterward is a data race."
- Options keep the signature open for future per-code metadata (e.g. a log-level hint) without another signature change.

---

## 4. The `Error` type

```go
type Error struct {
    code   Code        // Zero value OK means "unset"; CodeOf delegates to the inner Error.
    msg    string      // Internal message (for logs). Never shown to a client.
    public string      // Public message shown to clients. Empty means unset.
    cause  error       // The wrapped error. May be nil.
    pc     uintptr     // One recorded caller frame (resolved lazily). 0 means "none".
    attrs  []slog.Attr // Structured-logging attributes.
}
```

**Immutable**: no API mutates a field after construction. Every `With*` method returns a shallow copy, so it's safe under concurrent access.

### 4.1 Constructors

```go
// New creates a new error, recording one caller frame.
func New(code Code, msg string) *Error

// Newf is the fmt.Sprintf form of New. %w is not supported here (use Wrap
// to wrap an error).
func Newf(code Code, format string, args ...any) *Error

// Wrap wraps err, recording one caller frame.
// The code is left unset (OK), so CodeOf delegates to the Code further
// down the chain. To change the code, use Wrap(...).WithCode(c).
// Returns nil when err is nil, keeping chained builder calls safe. The
// return type is *Error, so a function declared to return error must keep
// its if err != nil guard — returning the nil *Error through an error
// interface yields a non-nil error (the typed-nil footgun).
func Wrap(err error, msg string) *Error

// Wrapf is the fmt.Sprintf form of Wrap. Likewise returns nil when err is nil.
func Wrapf(err error, format string, args ...any) *Error
```

Frame recording captures a single pc via a shared `caller()` helper — `runtime.Callers(3, pc[:1])`, skipping `runtime.Callers`, `caller`, and the constructor itself. Resolving it to `file:line` is deferred until display time (`runtime.CallersFrames`). This keeps construction cost down to tens of nanoseconds with minimal allocation.

### 4.2 Builder methods

All return a shallow copy of the receiver. **None re-record a frame** (recording is New/Wrap's job). Appending to attrs is done as "copy then append", avoiding sharing with the original slice (equivalent to `append(slices.Clip(e.attrs), ...)`).

```go
// WithCode returns a copy with the code replaced.
func (e *Error) WithCode(c Code) *Error

// WithPublic returns a copy with the public message set.
func (e *Error) WithPublic(msg string) *Error

// With returns a copy with the given slog.Attr values appended.
// Example: e.With(slog.String("user_id", id), slog.Int("attempt", n))
func (e *Error) With(attrs ...slog.Attr) *Error

// WithPublicField returns a copy with a public key-value field appended.
// Unlike With (attrs are internal, logs only), public fields are
// client-visible: problem.From emits them as RFC 9457 extension members.
// Never put internal data in a public field. Like the public message,
// they are excluded from LogValue.
func (e *Error) WithPublicField(key string, value any) *Error
```

Every builder method and every accessor below is **nil-receiver safe** (returns nil / a zero value when called on nil). This is specified because `Wrap` can return nil, and chained calls must not panic as a result.

### 4.3 The error interface and Unwrap

```go
// Error returns "msg: cause", or just msg if cause == nil.
// If msg is empty and cause is set, returns cause.Error() alone (no stray colon).
func (e *Error) Error() string

func (e *Error) Unwrap() error // returns cause
```

`Is` / `As` are not implemented separately. The standard `errors.Is/As` work by walking `Unwrap`. Code comparison is done explicitly via `CodeOf` — this library deliberately does not provide implicit matching like `errors.Is(err, errtrail.NotFound)`, since that behavior would be hard to predict.

---

## 5. Inspection API (inspect.go)

```go
// CodeOf walks err's chain from the outside in and returns the Code of the
// first *Error whose code != OK.
// Returns OK if err is nil, and Unknown if no *Error is found (or every
// *Error in the chain has an unset code).
// The walk follows both Unwrap() error and Unwrap() []error (errors.Join)
// depth-first.
func CodeOf(err error) Code

// IsRetryable reports whether err classifies a failure worth retrying,
// derived purely from CodeOf(err) — see (Code).Retryable for the built-in
// set. Returns false for nil and for non-errtrail errors (Unknown is
// conservatively not retryable). No errors.Is inspection is performed.
func IsRetryable(err error) bool

// PublicMessage walks the chain from the outside in and returns the first
// non-empty public message found. Falls back to
// http.StatusText(CodeOf(err).HTTPStatus()) if none is set
// (e.g. NotFound -> "Not Found"). Never falls back to an internal message.
//
// Note: http.StatusText returns "" for status codes it does not know —
// notably Canceled (499) and any custom code mapped to a non-standard HTTP
// status. For such a code with no explicitly-set public message,
// PublicMessage (and therefore the gRPC message and the problem Detail/Title)
// is the empty string. Set an explicit WithPublic on codes like these if a
// client-facing message is required.
func PublicMessage(err error) string

// Trace returns the frames of every *Error in the chain, ordered from the
// outermost (where it was last wrapped) to the innermost (where it
// originated). Returns nil if no *Error is found.
func Trace(err error) []Frame

// Attrs concatenates the attrs of every *Error in the chain, from outermost
// to innermost. Duplicate keys are not deduplicated (left to slog's own
// behavior).
func Attrs(err error) []slog.Attr

// PublicFields collects the public fields (WithPublicField) of every
// *Error in the chain into a map. For a duplicate key, the outermost value
// wins (consistent with CodeOf and PublicMessage: layers closer to the
// boundary override). Returns nil if none are found. Never includes attrs
// or internal messages.
func PublicFields(err error) map[string]any
```

### 5.1 Frame

```go
type Frame struct {
    Function string // Fully qualified function name, e.g. "example.com/app/repo.(*UserRepo).Get"
    File     string // Full path.
    Line     int
    Msg      string // The internal msg of the *Error that recorded this frame.
}

// String returns "Function (File:Line): Msg", omitting ": Msg" when Msg is empty.
func (f Frame) String() string
```

### 5.2 Shared chain-walking implementation

`CodeOf` / `PublicMessage` / `Trace` / `Attrs` all share the same walk function:

```go
// walk visits *Error values in err's chain depth-first (itself, then
// Unwrap; a Join visits its first branch first). Stops as soon as fn
// returns false.
func walk(err error, fn func(*Error) bool)
```

- Whenever an `errors.Join` / `Unwrap() []error` implementation is encountered, each element is recursed into in order.
- No cycle protection is needed (as with the standard errors package, a cyclic chain is the constructor's responsibility to avoid). Recursion depth is left to Go's own stack.

---

## 6. fmt.Formatter (format.go)

```go
func (e *Error) Format(s fmt.State, verb rune)
```

| verb | output |
|---|---|
| `%s`, `%v` | same as `e.Error()` |
| `%q` | `strconv.Quote(e.Error())` |
| `%+v` | the multi-line form below |

`%+v` output format (implement it exactly as shown):

```
get profile: query user: sql: no rows in result set
  code: NOT_FOUND
  public: User not found
  public.fields: resource=user
  attrs: user_id=42 attempt=3
  trace:
    example.com/app/service.(*UserService).Profile (/src/app/service/user.go:88): get profile
    example.com/app/repo.(*UserRepo).Get (/src/app/repo/user.go:42): query user
```

- Line 1: `e.Error()` (the concatenated message across the whole chain)
- `code:` line: `CodeOf(e).String()`
- `public:` line: printed only when a public message was explicitly set (never a fallback value)
- `public.fields:` line: omitted if no public fields; `key=value` pairs in walk order with duplicates kept (PublicFields' outermost-wins dedup applies only at response generation)
- `attrs:` line: omitted entirely if `Attrs(e)` is empty; `key=value` pairs separated by a single space
- `trace:` and below: one line per Frame in `Trace(e)`, via `Frame.String()`; the whole `trace:` section is omitted if empty
- Indentation is 2 spaces; trace entries are indented 4 spaces

---

## 7. slog integration (slog.go)

```go
// LogValue implements slog.LogValuer. Returns a group value.
func (e *Error) LogValue() slog.Value
```

Contents of the returned group:

| key | type | value |
|---|---|---|
| `msg` | string | `e.Error()` |
| `code` | string | `CodeOf(e).String()` |
| `trace` | []string | `Frame.String()` for each element of `Trace(e)` |
| (attrs) | — | `Attrs(e)`, spread directly into the group |

`public` and the public fields (`WithPublicField`) are never included in logs (logs are for internal use; both are exclusively for response generation).

Usage example:

```go
slog.Error("request failed", slog.Any("error", err))
// JSON: {"msg":"request failed","error":{"msg":"get profile: ...","code":"NOT_FOUND",
//        "trace":["...(user.go:88): get profile","..."],"user_id":42}}
```

Note: if the err passed to `slog.Any("error", err)` is not a `*Error` (a plain error instead), slog's standard behavior applies. The docs recommend wrapping with `errtrail.Wrap` before passing an error to a logger.

---

## 8. The problem package (RFC 9457)

`github.com/repenguin22/errtrail/problem`. Depends only on `encoding/json`, `net/http`, and the core `errtrail` package.

```go
// Problem is an RFC 9457 Problem Details object.
// Code is an extension member (RFC 9457 §3.2) conveying errtrail's Code
// name in machine-readable form.
type Problem struct {
    Type     string `json:"type,omitempty"`     // Omitted means "about:blank", per RFC.
    Title    string `json:"title"`
    Status   int    `json:"status"`
    Detail   string `json:"detail,omitempty"`
    Instance string `json:"instance,omitempty"` // URI for this specific occurrence.
    Code     string `json:"code"`

    // Extensions holds additional extension members (RFC 9457 §3.2),
    // flattened into the top-level JSON object by MarshalJSON. From fills
    // it with errtrail.PublicFields(err). Entries whose key is empty or
    // collides with a defined member (type, title, status, detail,
    // instance, code) are silently dropped.
    Extensions map[string]any `json:"-"`
}

// MarshalJSON flattens Extensions alongside the defined members. It must
// stay a value receiver: json.Marshal is called on Problem values, which
// would silently skip a pointer-receiver MarshalJSON.
func (p Problem) MarshalJSON() ([]byte, error)

// Option customizes the Problem built by From; applied last.
type Option func(*Problem)

// Instance sets the instance member — a URI identifying this specific
// occurrence, typically the request path. Boundary information (from the
// request), which is why it's an Option rather than stored on the error.
func Instance(uri string) Option

// From builds a Problem from err.
//   Status     = errtrail.CodeOf(err).HTTPStatus()
//   Title      = http.StatusText(Status), or the code name when http.StatusText
//                does not know the status (e.g. Canceled's 499)
//   Detail     = errtrail.PublicMessage(err) — empty if it equals Title (avoids redundancy)
//   Code       = errtrail.CodeOf(err).String()
//   Type       = TypeURL(CodeOf(err)) if TypeURL is set, otherwise empty
//   Extensions = errtrail.PublicFields(err)
// Never includes the internal message, attrs, or trace — extension members
// come only from data explicitly marked public via WithPublicField.
func From(err error, opts ...Option) Problem

// TypeURL is an optional hook that derives a type URI from a Code.
// A package variable. If you set it, do so before the server starts
// (concurrent writes are not supported).
var TypeURL func(errtrail.Code) string

// Write writes From(err, opts...) to w as application/problem+json.
//   - Sets the Content-Type: application/problem+json header
//   - Calls WriteHeader(p.Status)
//   - Returns any json.Marshal failure as an error rather than swallowing it
//     (reachable when a public field holds a value json cannot marshal;
//     writes a bare 500 in that case)
func Write(w http.ResponseWriter, err error, opts ...Option) error
```

Usage example:

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

## 9. The grpcerr package (separate module)

`github.com/repenguin22/errtrail/grpcerr`. Has its own go.mod and depends on `google.golang.org/grpc`. To let it depend on the core `errtrail` without a replace directive, the operating convention is: tag the core first, then tag grpcerr (using `grpcerr/vX.Y.Z`-style tags).

```go
// Domain opts in to attaching an errdetails.ErrorInfo to every non-OK
// status produced by ToStatus / ToError. Set it to the service's domain
// before the server starts (same race caveat as problem.TypeURL); empty
// (the default) attaches nothing, keeping the wire format unchanged.
var Domain string

// ToStatus converts err to a *status.Status.
//   code    = codes.Code(errtrail.CodeOf(err).GRPCCode())
//   message = errtrail.PublicMessage(err)
// When Domain is non-empty, attaches
// errdetails.ErrorInfo{Reason: code.String(), Domain: Domain} so the
// errtrail code name (e.g. a custom "RATE_LIMITED") survives the wire even
// though the numeric gRPC code may be shared. If details cannot be attached
// (WithDetails rejects OK statuses — a custom code may map to gRPC OK), the
// plain status is returned instead; the status itself is never lost.
// Returns status.New(codes.OK, "") when err is nil.
func ToStatus(err error) *status.Status

// ToError returns ToStatus(err).Err(), for returning directly from a gRPC handler.
// Returns nil when err is nil.
func ToError(err error) error
```

Further details (RetryInfo, BadRequest, ...) are the caller's job: `ToStatus` returns the `*status.Status`, so callers can chain their own `WithDetails` before `.Err()`. Automatic support may come later (see ROADMAP — the retryable flag exists as of core v0.3.0, but RetryInfo also needs a retry delay, which the registry doesn't carry; BadRequest can be built on the core's public fields, `WithPublicField` / `PublicFields`).

Usage example:

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

No interceptor is provided in v1 (it would end up tightly coupled to each service's own logging policy).

---

## 10. Edge case specification (summary)

| Case | Behavior |
|---|---|
| `Wrap(nil, ...)` / `Wrapf(nil, ...)` | returns `nil` |
| Calling a method on a nil receiver | `With*` returns nil. `Error()` returns `"<nil>"`. `Unwrap`/`LogValue`/etc. return their zero values |
| `CodeOf(nil)` | `OK` |
| `CodeOf(fmt.Errorf("x"))` (no `*Error` present) | `Unknown` |
| A chain built entirely with `Wrap` and no code set anywhere | Delegates to the innermost `*Error`'s code; `Unknown` if none has one |
| `PublicMessage` when public is unset | Falls back to `http.StatusText(HTTPStatus)`. Never falls back to the internal msg |
| `PublicMessage` on a code whose HTTP status has no `http.StatusText` (Canceled/499, or a custom non-standard status) with public unset | Returns `""` (so the gRPC message and the problem Detail/Title are empty too). Set `WithPublic` on these codes if a client message is needed |
| `errors.Join(a, b)` where both are `*Error` | Depth-first, first branch wins (`CodeOf`/`PublicMessage` take the first hit; `Trace`/`Attrs` collect every branch) |
| Using an unregistered custom code | `String()` returns `"CODE(n)"`, HTTP 500, gRPC UNKNOWN (2) |
| `Register(c < 100, ...)` / duplicate registration | panics |
| `New(OK, ...)` | Not forbidden (can't be caught by vet), but documented as discouraged. Since `CodeOf` skips an Error whose code == OK, it effectively resolves to Unknown |

---

## 11. Test plan

- **code_test.go**: table test covering all 17 built-in codes' mapping (HTTP/gRPC/String). Register's success and panic paths.
- **error_test.go**: immutability of New/Wrap/builders (the original Error is never mutated, attrs slices are never shared). Every nil-safety case. `errors.Is/As/Unwrap` compatibility (including mixed chains with the standard `%w`).
- **inspect_test.go**: covers every row of the edge-case table above. Walk order including `errors.Join`.
- **format_test.go**: compares `%s` / `%v` / `%q` / `%+v` output against golden strings (file/line matched via regex).
- **slog_test.go**: verifies actual JSON output via `slog.NewJSONHandler` plus a buffer.
- **problem_test.go**: verifies Content-Type / status / body via `httptest.ResponseRecorder`. Omission when Detail==Title.
- **grpcerr/grpcerr_test.go**: status conversion. gRPC mapping for a custom code.
- **Benchmarks** (bench_test.go): `New`, `Wrap`, building a 3-deep `Wrap` chain, `%+v` formatting. `New` targets roughly 1 alloc; `-benchmem` results are recorded in the README for regression detection.

## 12. Implementation order

1. `code.go` (+ tests) — standalone, no dependencies
2. `frame.go` → `error.go` (+ tests)
3. `inspect.go` (the walk implementation) (+ tests)
4. `format.go`, `slog.go` (+ tests)
5. `problem/` (+ tests)
6. Tag the core v0.1.0 → `grpcerr/` (+ tests) → tag `grpcerr/v0.1.0`
