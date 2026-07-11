# errtrail

[![CI](https://github.com/repenguin22/errtrail/actions/workflows/ci.yml/badge.svg)](https://github.com/repenguin22/errtrail/actions/workflows/ci.yml)

A Go error library for web services (HTTP / gRPC). See [DESIGN.md](DESIGN.md) for the full design spec.

- **Error codes are the source of truth** — `Code` values 0–16 match gRPC's `codes.Code`. HTTP / gRPC statuses and retryability are all derived from a lookup table.
- **Stdlib-only core** — the gRPC conversion is isolated in a separate `grpcerr` module.
- **Lightweight call-site tracking** — `New` / `Wrap` each record one caller frame. No stack traces.
- **Fully compatible with the standard `errors` package** — works with `Is` / `As` / `Unwrap` / `Join`.
- **Internal vs. public separation** — exactly three channels reach a client (public message, public fields, field violations); internal messages and attrs stay in your logs.
- **`slog.LogValuer` implementation** and **RFC 9457 (Problem Details)** responses, including extension members.
- **Same taxonomy end to end** — carry code names across gRPC and rebuild them on the calling side; retry delays (`RetryInfo`) and validation details (`BadRequest`) ride along automatically.

## Install

```
go get github.com/repenguin22/errtrail
go get github.com/repenguin22/errtrail/grpcerr   # only if you use gRPC
go get github.com/repenguin22/errtrail/otelerr   # only if you use OpenTelemetry
```

The core module supports **Go 1.22+**. The `grpcerr` and `otelerr` modules require **Go 1.25+**, following the minimums of `google.golang.org/grpc` and `go.opentelemetry.io/otel`.

## Usage

errtrail is built around one small discipline that mirrors the three places an
error passes through in a request:

```
  source  ─────▶  middle layers  ─────▶  boundary
 classify         add context           log once, respond
```

Each layer has exactly one job. Get these three right and everything else — the
HTTP/gRPC status, the client message, structured logs, retry decisions — falls
out of the `Code` for free. The rest of this section walks each layer in turn;
[Best practices](#best-practices) distils them into a checklist.

### 1. At the source — classify once

Where the error originates, attach everything you know in one place: a **`Code`**
(the classification, and the single source of truth for the HTTP/gRPC status), an
**internal message** for your own eyes, and — only if a client should see them —
a **public message**, structured **public fields**, and **field violations**
for validation failures. `With` adds `slog` attributes that stay in the logs.

```go
if row == nil {
    return errtrail.New(errtrail.NotFound, "user row missing").
        WithPublic("User not found").       // safe to show a client
        With(slog.String("user_id", id))    // logs only, never sent to a client
}
```

Errors from the standard library or a third-party package enter the chain the
moment you `Wrap` them — that is also where you assign their `Code`:

```go
if err := row.Scan(&u); err != nil {
    if errors.Is(err, sql.ErrNoRows) {
        return errtrail.Wrap(err, "user row missing").
            WithCode(errtrail.NotFound).
            WithPublic("User not found")
    }
    return errtrail.Wrap(err, "scan user").WithCode(errtrail.Internal)
}
```

You never write an HTTP status or a gRPC code by hand; both are derived from the
`Code` via a lookup table.

### 2. In middle layers — add context, don't reclassify

As the error travels up, `Wrap` it to record the path it took. Don't assign a new
code (the one from the source is inherited) and don't log yet — the trace already
remembers every layer.

```go
func (s *Service) Profile(ctx context.Context, id string) (*Profile, error) {
    u, err := s.repo.Get(ctx, id)
    if err != nil {
        return nil, errtrail.Wrap(err, "load profile") // context, not a new code
    }
    // ...
}
```

> **⚠️ Typed-nil footgun.** `Wrap` returns `*Error`, not `error`. Returning it
> unconditionally from a function whose signature is `error` produces a non-nil
> error holding a nil pointer, even on the success path. Keep the `if err != nil`
> guard (as above) rather than `return errtrail.Wrap(err, "...")` at the end of a
> function.

### 3. At the boundary — log once, then respond

At the top of the request (an HTTP handler or a gRPC method) do two things, each
exactly once: log the full internal error, then turn it into a response that
carries **only** public information.

```go
// HTTP handler
slog.ErrorContext(ctx, "request failed", slog.Any("error", err)) // everything internal
_ = problem.Write(w, err)                                        // only public data leaves

// gRPC handler
return nil, grpcerr.ToError(err)
```

The internal message, attrs, and trace never leave your process. The client gets
the public message and the machine-readable code name; when no public message
was set, HTTP clients see the generic status text as the problem title, and
gRPC clients get the code name (e.g. `"UNAVAILABLE"`) as the status message —
never HTTP wording, never an empty message.

If you use OpenTelemetry, mark the active span in the same breath — the
`otelerr` module records the error (code name + attrs, never the public
message) and sets the span status from the `Code`, following the OTel
server-span conventions: only server-fault codes (`Internal`, `Unavailable`,
`DeadlineExceeded`, …) mark the span as an error, so `NotFound`-heavy traffic
doesn't light your trace UI up:

```go
otelerr.Record(ctx, err) // exception event + span status derived from the Code

// And to join logs with traces, attach the IDs at the source:
err := errtrail.New(errtrail.Internal, "db write failed").
    With(otelerr.TraceAttrs(ctx)...)
```

For validation-style errors, attach **field violations** at the source with
`WithFieldViolation` — one typed channel that feeds both boundaries: the
`problem` package emits them as the `errors` extension member, and
`grpcerr.ToStatus` attaches them as an `errdetails.BadRequest`. `instance`
comes from the request at the boundary, not from the error:

```go
// At the source:
err := errtrail.New(errtrail.InvalidArgument, "email failed regexp").
    WithPublic("Validation failed").
    WithFieldViolation("email", "must be a valid email address")

// At the HTTP boundary:
_ = problem.Write(w, err, problem.Instance(r.URL.Path))
// {"code":"INVALID_ARGUMENT","detail":"Validation failed",
//  "errors":[{"field":"email","description":"must be a valid email address"}],
//  "instance":"/users","status":400,"title":"Bad Request"}

// At the gRPC boundary the same error yields InvalidArgument with an
// errdetails.BadRequest{FieldViolations: [{Field: "email", ...}]} detail.
```

Arbitrary structured extras beyond field violations still go through
`WithPublicField(key, value)`; they surface as RFC 9457 extension members
under their own key (an explicit `"errors"` public field overrides the
derived one). That override is HTTP-only by design: public fields never
reach the gRPC wire, so the `BadRequest` detail always derives from the
typed violations.

RFC 9457's `type` member (a URI reference identifying the problem type,
defaulting to `about:blank`) is also supported, but opt-in: it's omitted by
default because errtrail has no documentation site of its own to point `type`
at. Set `problem.TypeURL` once at startup to derive one from the `Code`:

```go
problem.TypeURL = func(c errtrail.Code) string {
    return "https://errors.example.com/" + c.String()
}
// Now From(err).Type == "https://errors.example.com/INVALID_ARGUMENT"
```

Over gRPC, set the service domain once at startup so custom code **names** (not
just their numeric gRPC code) survive the wire as a machine-readable
[`errdetails.ErrorInfo`](https://pkg.go.dev/google.golang.org/genproto/googleapis/rpc/errdetails#ErrorInfo):

```go
grpcerr.Domain = "myservice.example.com" // opt-in; empty (default) attaches nothing
```

### Calling other services

When your service is itself a client, bring the downstream error back into the
same taxonomy with `FromError`. Wire codes map one-to-one, and a custom code is
recovered from its `ErrorInfo.Reason` when you have registered the same code
locally. Retry decisions then derive from the same `Code`:

```go
res, err := client.GetUser(ctx, req)
if err != nil {
    terr := grpcerr.FromError(err)  // wire status -> errtrail.Code; wraps err
    if errtrail.IsRetryable(terr) { // Unavailable/DeadlineExceeded/ResourceExhausted/Aborted
        if d, ok := grpcerr.RetryDelay(err); ok { // server-recommended delay, if any
            return retryAfter(ctx, op, d)
        }
        return retryWithBackoff(ctx, op)
    }
    return nil, errtrail.Wrap(terr, "call user service")
}
```

Custom-code recovery requires the `ErrorInfo.Reason` to name a locally
registered code *and* its registered gRPC code to match the wire code. If the
client also talks to services outside your taxonomy, tighten it further with
`TrustedDomain` — recovery then additionally requires the `ErrorInfo.Domain`
to match:

```go
terr := grpcerr.FromError(err, grpcerr.TrustedDomain("user.example.com"))
```

There's no HTTP equivalent of `FromError` — gRPC codes map to `Code`
one-to-one, but an HTTP status is inherently many-to-one (`400` could be
`InvalidArgument`, `FailedPrecondition`, or `OutOfRange`), so a generic
`FromHTTPStatus` would guess wrong as often as it guessed right. If you call
another HTTP service, classify its response explicitly at the call site
instead (e.g. `errtrail.Wrap(err, "call user service").WithCode(...)`, picking
the `Code` from context you have and the library doesn't).

### Multi-error validation (`errors.Join`)

errtrail's inspection functions all walk an `errors.Join`-ed chain depth-first,
visiting the first branch first — the same rule used for a plain `Wrap` chain,
just applied per branch:

```go
a := errtrail.New(errtrail.InvalidArgument, "email invalid").WithFieldViolation("email", "must be valid")
b := errtrail.New(errtrail.InvalidArgument, "age invalid").WithFieldViolation("age", "must be >= 0")
joined := errors.Join(a, b)
```

- **`CodeOf`** returns the first **non-OK code** and **`PublicMessage`** the
  first **non-empty public message**, each in depth-first walk order — a
  single response can only carry one status and one message, so the first hit
  wins by convention. Note
  these are separate walks: when the first branch has a code but no public
  message, the message may come from a *different* branch than the code. That
  combination is intentional (each walk finds the best available value), but if
  the branches diverge, decide deliberately — set an explicit `WithCode` +
  `WithPublic` on the `Join` result rather than relying on which validator ran
  first.
- **`Trace` / `Attrs`** collect **every branch**, so nothing is lost from logs.
- **`FieldViolations` concatenates every branch** — it's a list, not a map, so
  joined validators never collide: both violations above reach the client
  (the `errors` member over HTTP, `BadRequest` over gRPC). This is why
  validation data belongs in violations.
- **`PublicFields` collects every branch too, but a duplicate key keeps only
  the first branch's value** — the same outermost-wins rule `Wrap` chains use;
  a `"field"` key set by both `a` and `b` would silently keep only `a`'s.
  Use distinct keys per branch, or — for anything list-shaped — use
  `WithFieldViolation`, which has no keys to collide.

### Inspecting the propagation path

`%+v` prints the full chain — message, code, public message, public fields and
field violations, attrs, and the recorded trace — for debugging and tests:

```
load profile: query user: sql: no rows in result set
  code: NOT_FOUND
  public: User not found
  public.fields: resource=user
  public.violations: user_id=does not exist
  attrs: user_id=42
  trace:
    example.com/app/service.(*UserService).Profile (/src/app/service/user.go:88): load profile
    example.com/app/repo.(*UserRepo).Get (/src/app/repo/user.go:42): query user
```

## Best practices

A checklist, with the reasoning behind each rule:

- **Attach the `Code` at the source, once.** It is the single source of truth: the
  HTTP status, the gRPC code, and `IsRetryable` all derive from it. Deriving a
  status by hand anywhere else is how they drift apart.
- **`Wrap` to add context; don't reclassify in the middle.** A middle layer that
  sets a new code hides the real cause. Let the source's code propagate; only
  override with `WithCode` when you are deliberately translating one failure into
  another (e.g. a downstream `Unavailable` you choose to surface as `Internal`).
  Beware: `WithCode` does **not** clear public data set below it — an inner
  `WithPublic("User not found")`, `WithPublicField`, or `WithFieldViolation`
  would still reach the client through the new response (a 403 hiding a
  NotFound must not carry the lookup's field violations either). When the
  point of reclassifying is to *hide* the original failure, add
  `WithoutPublic()` — it blocks all three public channels below it — then set
  a fresh `WithPublic` if needed. **Call it on a fresh `Wrap(err, ...)`, not
  on `err` itself**: the node you call it on keeps its *own* public data
  above the barrier, so `err.WithoutPublic()` still exposes everything
  attached directly to `err`.
- **Keep internal and public strictly separate.** Exactly **three channels**
  reach a client — `WithPublic`, `WithPublicField`, and `WithFieldViolation` —
  and nothing else ever does; the internal message and `With` attrs are for
  logs. Treat all three with the same care: a violation's field name and
  description go verbatim into the HTTP `errors` member and the gRPC
  `BadRequest`. When unsure whether a string is safe to expose, leave
  `WithPublic` unset — the client gets the generic status text (HTTP) or the
  code name (gRPC) rather than a leaked detail.
- **Classify with `CodeOf`; match sentinels with `errors.Is`.** For "what kind of
  failure is this?" switch on `errtrail.CodeOf(err)` — errtrail deliberately does
  *not* overload `errors.Is` for codes, because implicit code matching is hard to
  predict. `errors.Is` / `errors.As` still work for sentinel values (`sql.ErrNoRows`,
  your own `var Err…`) because `Wrap` preserves the cause.
- **Log once, at the boundary, with `slog.Any`.** The trace already records every
  `Wrap` point, so logging at each layer only duplicates it. Passing the `*Error`
  to `slog.Any("error", err)` expands its code, trace, and attrs via `LogValue`;
  the public message is intentionally omitted from logs.
- **Register custom codes at `init`.** Registration is safe at any time — the
  registry is swapped atomically (copy-on-write) — but registering at `init`
  keeps the taxonomy identical for every request the process ever serves. Give
  each custom code a unique `SCREAMING_SNAKE` name; it is the wire and config
  lookup key.
- **Use `Newf`/`Wrapf` only for the internal message.** Interpolated request data
  belongs in `With` attrs (queryable in logs) or `WithPublicField`, not baked into
  a message string.
- **Building an error factory? Use `NewSkip` / `WrapSkip`.** A helper like
  `apperr.NotFound(msg)` built on plain `New` records its own line in every
  trace, so all errors appear to originate at the factory. `NewSkip(1, ...)`
  skips the factory layer and points the frame at the real call site — `skip`
  counts wrapper layers, like zap's `AddCallerSkip`. (`grpcerr.FromError` does
  this internally, so its frame is your call site too.)
- **Retry on `IsRetryable`, not a hand-maintained list.** Built-ins `Unavailable`,
  `DeadlineExceeded`, `ResourceExhausted`, and `Aborted` are retryable; custom
  codes opt in with `errtrail.Register(c, name, httpStatus, grpcCode, errtrail.Retryable())`
  — or `errtrail.RetryAfter(d)`, which also ships a recommended delay to gRPC
  clients as a `RetryInfo` detail. It's a *transience hint* derived only from
  the `Code` — whether replaying the request is safe (idempotency, retry
  budget, server pushback) is still your call.

## Structured logging

`*Error` implements `slog.LogValuer`, so passing it to `slog.Any("error", err)` nests it as a structured group instead of a flat string — as long as `err` is a `*errtrail.Error` (wrap plain errors with `errtrail.Wrap` before logging them). The public message, public fields, and field violations are deliberately left out; they're for response generation, not logs.

```go
slog.New(slog.NewJSONHandler(os.Stdout, nil)).
    Error("request failed", slog.Any("error", err))
```

```json
{
  "time": "2026-07-07T23:25:18.408363+09:00",
  "level": "ERROR",
  "msg": "request failed",
  "error": {
    "msg": "load profile: query user: sql: no rows in result set",
    "code": "NOT_FOUND",
    "trace": [
      "main.main (/app/main.go:17): load profile",
      "main.main (/app/main.go:13): query user"
    ],
    "user_id": "42"
  }
}
```

## Packages

| Package | Dependencies | Role |
|---|---|---|
| `errtrail` | standard library only | Core. `Code`, `Error`, inspection, formatting, slog |
| `errtrail/problem` | standard library only | RFC 9457 response generation |
| `errtrail/grpcerr` | `google.golang.org/grpc` | `*status.Status` conversion (separate go.mod) |
| `errtrail/otelerr` | `go.opentelemetry.io/otel` | span recording / status from `Code` (separate go.mod) |

## Custom codes

Register your own codes (values `>= 100`; 0–99 are reserved), preferably from
`init`. Registration is thread-safe — the registry is replaced atomically
(copy-on-write), so lookups never observe a partial write even if you register
late — but `init` is still the recommended pattern so that every request sees
the same taxonomy.

> **HTTP-first services often need custom codes.** The 17 built-in codes are
> gRPC's, and `HTTPStatus` maps each one to a single fixed status — so they're
> a many-to-one mapping from the HTTP side. `InvalidArgument` is always `400`,
> for instance, even if your API would rather distinguish `400` from `422
> Unprocessable Entity`, or `AlreadyExists`/`Aborted` both landing on `409`
> when you'd want to tell them apart by status code alone. This doesn't break
> anything — register a custom code with whatever HTTP status you need — but
> if you're building an HTTP-only API, expect to reach for custom codes more
> often than the 17 built-ins alone would suggest.

```go
const RateLimited errtrail.Code = 100

func init() {
    errtrail.Register(
        RateLimited,
        "RATE_LIMITED",                 // unique SCREAMING_SNAKE name; the wire/config lookup key
        http.StatusTooManyRequests,     // HTTP status (must be in [400, 599] — a Code classifies an error)
        8,                              // gRPC code, ResourceExhausted (must be 1–16; 0 is OK)
        errtrail.Retryable(),           // optional: makes IsRetryable report true
    )
}
```

Instead of `Retryable()`, `RetryAfter(d)` marks the code retryable *and*
records a recommended delay: `grpcerr.ToStatus` then attaches an
`errdetails.RetryInfo` carrying it, and clients read it back with
`grpcerr.RetryDelay(err)`. (Built-ins can't carry a delay — they aren't
registered through `Register`.)

```go
errtrail.Register(Throttled, "THROTTLED", 429, 8, errtrail.RetryAfter(2*time.Second))
```

Once registered, `RateLimited` behaves like a built-in everywhere: `HTTPStatus`,
`GRPCCode`, `String`, `Retryable`, `problem.From`, and `grpcerr.ToStatus` all
resolve it through the same table, and `CodeByName("RATE_LIMITED")` recovers it
(used by `grpcerr.FromError` to rebuild the code from the wire).

`Register` panics on misuse — a code below 100, a duplicate code or name, a
name that doesn't match `[A-Z][A-Z0-9_]+[A-Z0-9]` (≤ 63 chars, the
`ErrorInfo.Reason` wire constraints), an HTTP status outside `[400, 599]`, or
a gRPC code outside `[1, 16]` — so a mistake surfaces at startup rather than
mid-request.

## Benchmarks

Apple M1 Pro, Go 1.26, as of v1.1. `New` / `Wrap` are still 1 alloc each,
including frame recording (the struct grew with v1.1's field-violation and
barrier support — hence 144 B, up from 96 B pre-v1.1).

```
BenchmarkNew-10          11877836    201.8 ns/op    144 B/op   1 allocs/op
BenchmarkWrap-10         11174144    198.5 ns/op    144 B/op   1 allocs/op
BenchmarkWrapChain3-10    3570674    645.4 ns/op    432 B/op   3 allocs/op
BenchmarkFormatPlusV-10   1000000   2085   ns/op   3489 B/op  25 allocs/op
```

## Versioning and stability

The three modules are **versioned independently** under [Semantic Versioning](https://semver.org/), each with its own tag line:

```
git tag v0.1.0            # core
git tag grpcerr/v0.1.0    # grpcerr submodule
git tag otelerr/v0.1.0    # otelerr submodule
```

A submodule's `require` points at a tagged core version (no `replace`); when developing several modules together, put a `go.work` outside the repo. See [CHANGELOG.md](CHANGELOG.md) for the full history.

**All three modules are v1.0+ and follow the SemVer compatibility promise:**
no breaking change to the public API or to documented wire/response behavior
without a major version bump. A minor version may add API surface; a patch is
fixes only. Behavior changes of any kind are called out in the changelog.

### The road to v1.0 (record)

v1.0 was a promise to keep the settled feature surface stable. The criteria,
all met before tagging:

- [x] P1 feature set complete (public fields, retryability, gRPC round-trip, OTel)
- [x] Registry is thread-safe; CI gates per-module coverage
- [x] The `problem.TypeURL` / `grpcerr.Domain` "set before startup" contract is decided (documented as final — they are plain package variables with no partial-write hazard)
- [x] gRPC wire-level round-trip test — a real `grpc.Server`/`ClientConn` over bufconn verifies that `ErrorInfo` details survive the actual transport (`grpcerr/e2e_test.go`)
- [x] Pre-v1.0 semantics fixes from the final review round (2026-07-10, core v0.7.0 / grpcerr v0.5.0): `WithoutPublic()`, tightened `Register` validation, `PublicFields` last-write-wins within a node, the `PublicMessage` code-name fallback, `FromOption`/`TrustedDomain`, and ErrorInfo only for registered codes — every change that would have been breaking after v1.0

## License

[MIT](LICENSE)
