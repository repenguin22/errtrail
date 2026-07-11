# errtrail — Design Spec

A Go error library for web services (HTTP / gRPC).

- Status: Living document — current as of v1.1
- Module: `github.com/repenguin22/errtrail`
- Go: 1.22+ (1.21 is the floor imposed by `log/slog`; development targets 1.22)

---

## 1. Goals and non-goals

### Goals

1. **Error codes are the source of truth.** Codes are represented by a custom `Code` type aligned with gRPC's 16 codes (`codes.Code`); HTTP and gRPC statuses are derived from it via a lookup table.
2. **The core depends on the standard library only.** Only the conversion to gRPC's `*status.Status` is isolated in a separate go.mod submodule, `grpcerr`.
3. **Track the origin and propagation path of an error.** `New` / `Wrap` each record just one caller frame. No full stack traces.
4. **Fully compatible with the standard `errors` package.** Works alongside `errors.Is` / `errors.As` / `errors.Unwrap` / `errors.Join`.
5. **Separate internal data from client-visible data.** Exactly three explicitly-set channels ever reach a client — the public message (`WithPublic`), public extension fields (`WithPublicField`), and field violations (`WithFieldViolation`); internal messages and attrs never do.
6. **Structured logging integration.** Implements `slog.LogValuer`.
7. **RFC 9457 (Problem Details) HTTP response generation**, provided via the `problem` subpackage.

### Non-goals (out of scope for v1)

- Metadata such as a log-level hint — a retryable flag is supported as of v0.3.0 (`IsRetryable` / `Register`'s `Retryable` option, §3.3), and a retry delay as of v1.1 (`RetryAfter`, §3.3)
- gRPC's rich `errdetails` beyond the opt-in set — `ErrorInfo` (via `grpcerr.Domain`), `RetryInfo` (via `RetryAfter`), and `BadRequest` (via `WithFieldViolation`) are attached automatically as of v1.1 (§9); anything further (LocalizedMessage, Help, ...) stays the caller's `WithDetails` job
- Reverse conversion from HTTP status back to `Code` — unlike gRPC's status codes (which map to `Code` one-to-one, making `grpcerr.FromError`/`FromStatus` well-defined, §9), an HTTP status is inherently many-to-one (e.g. `400` could be `InvalidArgument`, `FailedPrecondition`, or `OutOfRange`), so a generic reverse mapping would be lossy and guess wrong as often as not
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
├── grpcerr/
│   ├── go.mod             // module github.com/repenguin22/errtrail/grpcerr (deps: google.golang.org/grpc)
│   └── grpcerr.go
└── otelerr/
    ├── go.mod             // module github.com/repenguin22/errtrail/otelerr (deps: go.opentelemetry.io/otel)
    └── otelerr.go
```

`problem` can be written using only the standard library, so it lives in the core module. `grpcerr` and `otelerr` carry heavy third-party dependencies, so each is a separate module — users who don't need them never pull those dependencies in.

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
// A transience hint only — see IsRetryable.
func (c Code) Retryable() bool

// RetryDelay returns the recommended retry delay registered via the
// RetryAfter option, reporting whether one was set. Built-ins (not
// registered through Register), unregistered codes, and custom codes
// without RetryAfter report false.
func (c Code) RetryDelay() (time.Duration, bool)

// CodeByName returns the Code registered under name — built-ins included,
// e.g. "NOT_FOUND". Reports false for names it does not know. Useful for
// parsing code names from configuration or a wire format (grpcerr's
// ErrorInfo.Reason recovery).
func CodeByName(name string) (Code, bool)
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
// A transience hint only — see IsRetryable.
func Retryable() RegisterOption

// RetryAfter returns a RegisterOption recording a recommended retry delay
// (readable via (Code).RetryDelay; grpcerr.ToStatus attaches it as an
// errdetails.RetryInfo). A delay is only meaningful for a failure worth
// retrying, so it also implies the retryable flag. Panics during Register
// on a non-positive delay.
func RetryAfter(d time.Duration) RegisterOption

// Register adds a custom code. Safe to call at any time, including
// concurrently with lookups — the registry snapshot is replaced atomically
// (copy-on-write). Registering from init is still the recommended pattern
// so every request sees the same taxonomy.
// Panics if c < 100, if the code or the name is already registered (names
// are the CodeByName reverse-lookup key, so they must be unique), if name
// does not match [A-Z][A-Z0-9_]+[A-Z0-9] / exceeds 63 chars (the
// ErrorInfo.Reason wire constraints), if httpStatus is outside [400, 599]
// (a Code classifies an error; 2xx/3xx would make problem.Write emit a
// success response), or if grpcCode is outside [1, 16] (0 is OK — the error
// would vanish through grpcerr.ToError).
func Register(c Code, name string, httpStatus int, grpcCode uint32, opts ...RegisterOption)
```

- 0–99 are reserved (currently only 0–16 are used; the rest is held for future built-ins). Custom codes start at 100.
- Implemented as an immutable `registry` snapshot (`codes map[Code]codeInfo` — `codeInfo{name string; httpStatus int; grpcCode uint32; retryable bool; retryDelay time.Duration}` — plus a `names map[string]Code` reverse index for CodeByName) behind an `atomic.Pointer[registry]`. Readers do one atomic load and a map lookup; `Register` takes a writer-only mutex, clones the snapshot, adds the entry to both maps, and swaps the pointer. A lookup therefore never observes a partial write, and late registration is safe rather than undefined behavior. The 17 built-ins seed the first snapshot at init, so lookups go through a single path. Composite conversions (reading status, then name, then retryable) are race-free but not linearizable across a concurrent registration — a request in flight during a late registration can mix pre- and post-registration answers, which is one more reason to register at init. No snapshot-consistent lookup API is provided: entries are immutable once registered, so the window is that single in-flight request.
- Options keep the signature open for future per-code metadata (e.g. a log-level hint) without another signature change.

---

## 4. The `Error` type

```go
type Error struct {
    code       Code             // Zero value OK means "unset"; CodeOf delegates to the inner Error.
    msg        string           // Internal message (for logs). Never shown to a client.
    public     string           // Public message shown to clients. Empty means unset.
    cause      error            // The wrapped error. May be nil.
    pc         uintptr          // One recorded caller frame (resolved lazily). 0 means "none".
    attrs      []slog.Attr      // Structured-logging attributes (internal, logs only).
    fields     []publicField    // Public extension fields (client-visible).
    violations []FieldViolation // Field-level validation violations (client-visible).

    // noPublicBelow marks this node as a public-data barrier (WithoutPublic):
    // the cause chain below it contributes no public message, fields, or
    // violations.
    noPublicBelow bool
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

Frame recording captures a single pc via a shared `caller()` helper — `runtime.Callers(3, pc[:1])`, skipping `runtime.Callers`, `caller`, and the constructor itself. Resolving it to `file:line` is deferred until display time (`runtime.CallersFrames`). This keeps construction cost to roughly 200ns / 1 alloc (as of v1.1; see the README benchmarks).

### 4.2 Builder methods

All return a shallow copy of the receiver. **None re-record a frame** (recording is New/Wrap's job). Appending to attrs is done as "copy then append", avoiding sharing with the original slice (equivalent to `append(slices.Clip(e.attrs), ...)`). Attr and field *values* are stored by reference (no deep copy) — callers hand over an immutable snapshot; mutating a passed map or slice later changes what the boundary emits, and doing so concurrently is a data race.

```go
// WithCode returns a copy with the code replaced.
func (e *Error) WithCode(c Code) *Error

// WithPublic returns a copy with the public message set.
func (e *Error) WithPublic(msg string) *Error

// WithoutPublic returns a copy that acts as a public-data barrier: the
// cause chain below the node contributes no public message, no public
// fields, and no field violations. The node's own public data — and
// anything added by an outer wrap — still applies; internal msg, attrs,
// and trace are unaffected. For reclassification that must hide the
// original failure (NotFound -> PermissionDenied).
func (e *Error) WithoutPublic() *Error

// With returns a copy with the given slog.Attr values appended.
// Example: e.With(slog.String("user_id", id), slog.Int("attempt", n))
func (e *Error) With(attrs ...slog.Attr) *Error

// WithPublicField returns a copy with a public key-value field appended.
// Unlike With (attrs are internal, logs only), public fields are
// client-visible: problem.From emits them as RFC 9457 extension members.
// Never put internal data in a public field. Like the public message,
// they are excluded from LogValue.
func (e *Error) WithPublicField(key string, value any) *Error

// WithFieldViolation returns a copy with a FieldViolation{Field,
// Description} appended — the typed channel for field-level validation
// violations. Client-visible like the public message and fields: problem
// emits them as the "errors" extension member, grpcerr as an
// errdetails.BadRequest; excluded from LogValue, blocked below a
// WithoutPublic barrier.
func (e *Error) WithFieldViolation(field, description string) *Error
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
// A transience hint only: replay safety (idempotency, retry budgets,
// server pushback) remains the caller's responsibility.
func IsRetryable(err error) bool

// LookupPublicMessage walks the chain from the outside in and returns the
// first explicitly-set public message, reporting whether one was found. It
// never falls back — for callers that want their own fallback policy
// (grpcerr.ToStatus falls back to the code name; problem.From leaves the
// detail empty because title already carries the generic wording; an i18n
// layer might pick a translation). Public messages below a WithoutPublic
// barrier are not considered.
func LookupPublicMessage(err error) (string, bool)

// PublicMessage is LookupPublicMessage plus a two-level fallback:
// http.StatusText(CodeOf(err).HTTPStatus()) (e.g. NotFound -> "Not Found"),
// then — when http.StatusText does not know the status, notably Canceled's
// 499 and custom codes on a non-standard HTTP status — the code name (e.g.
// "CANCELED"), the same rule the problem title and the gRPC status message
// use. Never returns "" for a non-nil err; never falls back to an internal
// message.
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
// *Error in the chain into a map. For a duplicate key, the outermost
// *Error's value wins (consistent with CodeOf and PublicMessage: layers
// closer to the boundary override); within one *Error, the last
// WithPublicField wins (consistent with calling WithPublic twice). Fields
// below a WithoutPublic barrier are not collected. Returns nil if none are
// found. Never includes attrs or internal messages.
func PublicFields(err error) map[string]any

// FieldViolations concatenates every violation (WithFieldViolation) in
// walk order (outermost first; Join branches depth-first). A list, not a
// map — nothing is deduplicated or overridden; layers and Join branches
// each contribute their own. Violations below a WithoutPublic barrier are
// not collected. Returns nil if none are found.
func FieldViolations(err error) []FieldViolation
```

### 5.1 Frame

```go
type Frame struct {
    Function string // Fully qualified function name, e.g. "example.com/app/repo.(*UserRepo).Get"
    File     string // Full path.
    Line     int
    Msg      string // The internal msg of the *Error that recorded this frame.
}

// String returns "Function (File:Line): Msg". It omits ": Msg" when Msg is
// empty, and " (File:Line)" when both File and Line are zero (an unresolved
// frame, e.g. from a zero-value Error).
func (f Frame) String() string
```

### 5.2 Shared chain-walking implementation

`CodeOf` / `LookupPublicMessage` / `PublicMessage` / `PublicFields` / `FieldViolations` / `Trace` / `Attrs` all share the same walk function:

```go
// walk visits *Error values in err's chain depth-first (itself, then
// Unwrap; a Join visits its first branch first). Stops as soon as fn
// returns false. fn's blocked argument reports whether the node sits below
// a WithoutPublic barrier — only the public-data collectors consult it.
// Blocking is per subtree, threaded through the Join recursion, so a
// barrier in one branch never blocks a sibling branch.
func walk(err error, fn func(e *Error, blocked bool) bool)
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
  public.violations: user_id=does not exist
  attrs: user_id=42 attempt=3
  trace:
    example.com/app/service.(*UserService).Profile (/src/app/service/user.go:88): get profile
    example.com/app/repo.(*UserRepo).Get (/src/app/repo/user.go:42): query user
```

- Line 1: `e.Error()` (the concatenated message across the whole chain)
- `code:` line: `CodeOf(e).String()`
- `public:` line: printed only when a public message was explicitly set (never a fallback value)
- `public.fields:` line: omitted if no public fields; `key=value` pairs in walk order with duplicates kept (PublicFields' outermost-wins dedup applies only at response generation)
- `public.violations:` line: omitted if no field violations; `field=description` pairs in walk order (matches FieldViolations)
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

`public`, the public fields (`WithPublicField`), and the field violations (`WithFieldViolation`) are never included in logs (logs are for internal use; all three are exclusively for response generation).

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
//   Detail     = the explicitly-set public message (errtrail.LookupPublicMessage),
//                or empty if none is set or it equals Title (avoids redundancy)
//   Code       = errtrail.CodeOf(err).String()
//   Type       = TypeURL(CodeOf(err)) if TypeURL is set, otherwise empty
//   Extensions = errtrail.PublicFields(err), plus an "errors" member holding
//                errtrail.FieldViolations(err) as [{"field","description"}]
//                when the error carries violations (an explicit
//                WithPublicField("errors", ...) wins — key presence decides,
//                so even an explicit nil suppresses the derived member and
//                serializes as "errors": null)
// Never includes the internal message, attrs, or trace — extension members
// come only from data explicitly marked public via WithPublicField /
// WithFieldViolation.
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
//   message = the explicitly-set public message (errtrail.LookupPublicMessage),
//             or the code name (e.g. "UNAVAILABLE", "RATE_LIMITED") when none
//             is set — keeps HTTP wording off the gRPC wire and the message
//             non-empty for codes whose HTTP status has no standard text
// When Domain is non-empty, attaches
// errdetails.ErrorInfo{Reason: code.String(), Domain: Domain} so the
// errtrail code name (e.g. a custom "RATE_LIMITED") survives the wire even
// though the numeric gRPC code may be shared. Attached only for registered
// codes (CodeByName resolves) — an unregistered code's "CODE(123)" violates
// the Reason spec and can't round-trip, so it ships plain.
// Independent of Domain, also attaches an errdetails.RetryInfo when the
// code was registered with RetryAfter, and an errdetails.BadRequest built
// from the error's field violations — fixed order ErrorInfo, RetryInfo,
// BadRequest; errors without that data keep the plain wire format. If
// details cannot be attached (a proto marshal failure), the plain status is
// returned instead; the status itself is never lost.
// Returns status.New(codes.OK, "") when err is nil.
func ToStatus(err error) *status.Status

// ToError returns ToStatus(err).Err(), for returning directly from a gRPC handler.
// Returns nil when err is nil.
func ToError(err error) error

// FromError converts an error returned by a gRPC call into an
// *errtrail.Error that wraps it (errors.Is/As keep seeing the original).
// nil returns nil. Wire codes 0–16 map one-to-one; codes above 16 and
// non-status errors become Unknown. A custom code is recovered from an
// errdetails.ErrorInfo whose Reason names a locally registered code AND
// whose registered gRPC code matches the wire code — the second condition
// guards against foreign taxonomies reusing a local code name. The default
// ignores ErrorInfo.Domain; the TrustedDomain(domains ...string) FromOption
// additionally requires the Domain to match one of the given domains. The
// wire message survives via the wrapped cause; it is NOT re-published as
// the public message (call WithPublic explicitly to propagate it). The
// recorded frame points inside grpcerr — wrap at the call site for a
// caller frame.
func FromError(err error, opts ...FromOption) *errtrail.Error

// FromStatus is FromError for a *status.Status you already hold.
// Returns nil when st is nil or its code is OK.
func FromStatus(st *status.Status, opts ...FromOption) *errtrail.Error

// RetryDelay returns the delay carried by the first errdetails.RetryInfo
// detail on err's gRPC status that holds a POSITIVE delay, reporting
// whether one was found. (0, false) for nil, non-status errors, statuses
// without the detail, and RetryInfo whose delay is unset, zero, or negative
// ("retry after zero" carries no recommendation; a later RetryInfo with a
// positive delay still wins). A hint, like IsRetryable — replay safety
// stays with the caller. FromError deliberately does not turn received
// details back into public data on the returned error.
func RetryDelay(err error) (time.Duration, bool)
```

RetryInfo and BadRequest are attached automatically as of v1.1 (ROADMAP §3): the delay comes from the registry (`RetryAfter` RegisterOption, read via `(Code).RetryDelay`), the violations from the error (`WithFieldViolation` / `FieldViolations`). `ToStatus`/`ToError` deliberately gained no options — the configuration lives in the registry and on the error, and adding a variadic parameter post-v1.0 would be a breaking change. Callers needing other details still chain their own `WithDetails` before `.Err()`.

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

## 10. The otelerr package (separate module)

`github.com/repenguin22/errtrail/otelerr`. Has its own go.mod and depends on `go.opentelemetry.io/otel` (+ `otel/trace`; the SDK is test-only). Named `otelerr` — not `otel` — so the package name doesn't clash with `go.opentelemetry.io/otel`, mirroring the `grpcerr` naming. Follows the same release convention: tag the core first, then `otelerr/vX.Y.Z`.

```go
// Record records err on the span in ctx: an exception event carrying the
// errtrail code name ("errtrail.code") and the With attrs, plus — for
// server-fault codes only — a span status of Error with err.Error() as the
// description. No-op when err is nil or ctx carries no recording span.
func Record(ctx context.Context, err error)

// RecordSpan is Record for a span you already hold.
func RecordSpan(span trace.Span, err error)

// TraceAttrs returns slog attrs (trace_id, span_id) for the span in ctx,
// for attaching to an error via With so logs can be joined with traces.
// Returns nil when ctx has no valid span context.
func TraceAttrs(ctx context.Context) []slog.Attr
```

Design decisions:

- **Spans are an internal channel, like logs — not the public channel.** Span attributes are exported only to your own tracing backend and never propagate on the wire (only the trace-context IDs do), so their exposure equals the log pipeline's. Record therefore carries the internal message chain (`err.Error()`), the code name, and the attrs, and — exactly like `LogValue` — leaves the public message out.
- **Span status follows the OTel gRPC semantic conventions for server spans**: only server-fault codes set `Error` — UNKNOWN, DEADLINE_EXCEEDED, UNIMPLEMENTED, INTERNAL, UNAVAILABLE, DATA_LOSS. Client-fault codes (NOT_FOUND, INVALID_ARGUMENT, ...) leave the status unset so lookup misses don't read as an error storm in the trace UI; the exception event is recorded either way. Custom codes map through their registered `GRPCCode()`, keeping the registry the single source of truth. A plain (non-errtrail) error resolves to UNKNOWN and is conservatively a server fault.
- The slog attr conversion maps String/Int64/Float64/Bool to native attribute types and degrades everything else (Duration, Time, Group, Any) to the slog string form; LogValuer values are resolved first.

---

## 11. Edge case specification (summary)

| Case | Behavior |
|---|---|
| `Wrap(nil, ...)` / `Wrapf(nil, ...)` | returns `nil` |
| Calling a method on a nil receiver | `With*` returns nil. `Error()` returns `"<nil>"`. `Unwrap`/`LogValue`/etc. return their zero values |
| `CodeOf(nil)` | `OK` |
| `CodeOf(fmt.Errorf("x"))` (no `*Error` present) | `Unknown` |
| A chain built entirely with `Wrap` and no code set anywhere | Delegates to the innermost `*Error`'s code; `Unknown` if none has one |
| `PublicMessage` when public is unset | Two-level fallback: `http.StatusText(HTTPStatus)`, then the code name when StatusText is empty. Never falls back to the internal msg, never returns `""` for a non-nil err |
| `LookupPublicMessage` when public is unset | Returns `("", false)` — no fallback of any kind |
| Code whose HTTP status has no `http.StatusText` (Canceled/499, or a custom non-standard status) with public unset | `PublicMessage`, the gRPC message, and the problem Title all fall back to the code name (`"CANCELED"`) — the library never hands `""` to a client |
| `errors.Join(a, b)` where both are `*Error` | Depth-first, first branch wins (`CodeOf`/`PublicMessage` take the first hit; `Trace`/`Attrs`/`FieldViolations` collect every branch; `PublicFields` collects every branch but keeps the first branch's value for a duplicate key) |
| `WithoutPublic()` on a node | The cause chain below it contributes no public message/fields/violations; the node's own public data and outer wraps still apply; internal msg/attrs/trace and `CodeOf` unaffected. In a Join, a barrier inside one branch never blocks a sibling branch |
| Field violations (`WithFieldViolation`) | A list, not a map: `FieldViolations` concatenates every layer and Join branch in walk order, nothing deduplicated. Client-visible (problem `"errors"` member, gRPC `BadRequest`), excluded from logs |
| Using an unregistered custom code | `String()` returns `"CODE(n)"`, HTTP 500, gRPC UNKNOWN (2) |
| `Register(c < 100, ...)` / duplicate registration | panics |
| `New(OK, ...)` | Not forbidden (can't be caught by vet), but documented as discouraged. Since `CodeOf` skips an Error whose code == OK, it effectively resolves to Unknown |

---

## 12. Test plan

- **code_test.go**: table test covering all 17 built-in codes' mapping (HTTP/gRPC/String). Register's success, boundary, and panic paths — including the v1.1 `RetryAfter` option (retryable implication, `RetryDelay` accessor, non-positive panic).
- **error_test.go**: immutability of New/Wrap/builders (the original Error is never mutated; attrs, fields, and violations slices never share appendable capacity). Every nil-safety case. `errors.Is/As/Unwrap` compatibility (including mixed chains with the standard `%w`).
- **inspect_test.go**: covers every row of the edge-case table above. Walk order including `errors.Join`. `WithoutPublic` barrier placement (own node, fresh Wrap, Join positions) across all three public channels; `FieldViolations` concatenation.
- **format_test.go**: compares `%s` / `%v` / `%q` / `%+v` output against golden strings (file/line matched via regex), including the `public.fields:` / `public.violations:` lines and their omission below a barrier.
- **slog_test.go**: verifies actual JSON output via `slog.NewJSONHandler` plus a buffer; asserts the three public channels never appear in logs.
- **problem_test.go**: verifies Content-Type / status / body via `httptest.ResponseRecorder`. Omission when Detail==Title. The derived `"errors"` member, the explicit-override rule (incl. nil), the barrier, and an adversarial serialized-body no-leak test.
- **grpcerr/grpcerr_test.go + e2e_test.go**: status conversion; gRPC mapping for a custom code; ErrorInfo/RetryInfo/BadRequest attach conditions and order; `TrustedDomain`; `RetryDelay` (positive-only); barrier; a bufconn wire round-trip for all three details.
- **Benchmarks** (bench_test.go): `New`, `Wrap`, building a 3-deep `Wrap` chain, `%+v` formatting. `New` targets roughly 1 alloc; `-benchmem` results are recorded in the README for regression detection.

## 13. Implementation order

1. `code.go` (+ tests) — standalone, no dependencies
2. `frame.go` → `error.go` (+ tests)
3. `inspect.go` (the walk implementation) (+ tests)
4. `format.go`, `slog.go` (+ tests)
5. `problem/` (+ tests)
6. Tag the core v0.1.0 → `grpcerr/` (+ tests) → tag `grpcerr/v0.1.0`
