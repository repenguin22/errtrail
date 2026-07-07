# errtrail

[![CI](https://github.com/repenguin22/errtrail/actions/workflows/ci.yml/badge.svg)](https://github.com/repenguin22/errtrail/actions/workflows/ci.yml)

A Go error library for web services (HTTP / gRPC). See [DESIGN.md](DESIGN.md) for the full design spec.

- **Error codes are the source of truth** — `Code` values 0–16 match gRPC's `codes.Code`. HTTP / gRPC statuses are derived from a lookup table.
- **Stdlib-only core** — the gRPC conversion is isolated in a separate `grpcerr` module.
- **Lightweight call-site tracking** — `New` / `Wrap` each record one caller frame. No stack traces.
- **Fully compatible with the standard `errors` package** — works with `Is` / `As` / `Unwrap` / `Join`.
- **Internal vs. public message separation** — clients only ever see the public message.
- **`slog.LogValuer` implementation** and **RFC 9457 (Problem Details)** responses.

## Install

```
go get github.com/repenguin22/errtrail
go get github.com/repenguin22/errtrail/grpcerr   # only if you use gRPC
```

The core module supports **Go 1.22+**. The `grpcerr` module requires **Go 1.25+**, following the minimum of `google.golang.org/grpc`.

## Usage

```go
// At the source: attach a code, an internal message, a public message, and attributes.
err := errtrail.New(errtrail.NotFound, "user row missing").
    WithPublic("User not found").
    With(slog.String("user_id", id))

// In a middle layer: wrap to add context. The code is inherited from below.
if err != nil {
    return errtrail.Wrap(err, "load profile")
}
```

At the boundary:

```go
// HTTP handler
slog.ErrorContext(ctx, "request failed", slog.Any("error", err)) // log everything internal
_ = problem.Write(w, err)                                        // only the public message reaches the client

// gRPC handler
return nil, grpcerr.ToError(err)
```

Inspect the propagation path with `%+v`:

```
load profile: query user: sql: no rows in result set
  code: NOT_FOUND
  public: User not found
  attrs: user_id=42
  trace:
    example.com/app/service.(*UserService).Profile (/src/app/service/user.go:88): load profile
    example.com/app/repo.(*UserRepo).Get (/src/app/repo/user.go:42): query user
```

## Structured logging

`*Error` implements `slog.LogValuer`, so passing it to `slog.Any("error", err)` nests it as a structured group instead of a flat string — as long as `err` is a `*errtrail.Error` (wrap plain errors with `errtrail.Wrap` before logging them). `public` is deliberately left out; it's for response generation, not logs.

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

## Custom codes

Register values >= 100 from `init` or before the server starts (registering after startup is not supported).

```go
const RateLimited errtrail.Code = 100

func init() {
    errtrail.Register(RateLimited, "RATE_LIMITED", http.StatusTooManyRequests, 8 /* ResourceExhausted */)
}
```

## Benchmarks

Apple M-series, Go 1.26. `New` / `Wrap` are 1 alloc each, including frame recording.

```
BenchmarkNew-10          8312416    141.2 ns/op    96 B/op   1 allocs/op
BenchmarkWrap-10         8593356    148.6 ns/op    96 B/op   1 allocs/op
BenchmarkWrapChain3-10   2671467    441.1 ns/op   288 B/op   3 allocs/op
BenchmarkFormatPlusV-10   869619   1329   ns/op  3345 B/op  24 allocs/op
```

## Release process

The core and `grpcerr` are separate modules, so tag them independently.

```
git tag v0.1.0            # core
git tag grpcerr/v0.1.0    # grpcerr submodule
```

`grpcerr`'s `require` points at a tagged core version (no replace). When developing both modules together, put a `go.work` outside the repo that uses both modules to get local references.

## License

[MIT](LICENSE)
