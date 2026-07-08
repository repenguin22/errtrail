# Roadmap

Prioritized feature work toward production readiness / v1.0, split out of the
2026-07-08 review. Each item is intended to be a separate, self-contained
change (its own branch / PR).

## P1 — features production users will ask for first

### 1. gRPC errdetails support (grpcerr)

Today only the numeric gRPC code and the public message survive the wire;
custom code names and attrs are lost. Attach
`errdetails.ErrorInfo{Reason: code.String()}` in `ToStatus`, and consider
opt-in `RetryInfo` / `BadRequest` support.

- Scope: `grpcerr` module only (`errdetails` lives in
  `google.golang.org/genproto`, already an indirect dependency).
- Note: listed as a v1 non-goal in DESIGN.md §1 — update that section when
  this lands.
- Public-vs-internal rule still applies: only the code name and other
  explicitly public data may go into details, never `msg`/attrs by default.

### 2. RFC 9457 extension members (problem)

Real REST APIs quickly need field-level validation details and `instance`.
Provide a way to attach **public** extension fields, kept separate from the
(internal-only) slog attrs — e.g. a `WithPublicField(key, value)` builder on
`*Error`, or functional options on `problem.From`.

- Must preserve the internal/public separation: extension members are
  client-visible, so they need their own explicit channel; reusing `With`
  attrs would leak log data.
- Include `instance` support (likely a `problem.From` option fed from the
  request path, not something stored on the error).

### 3. Retryability helper

`IsRetryable(err) bool` derived from Code — `Unavailable`,
`DeadlineExceeded`, `ResourceExhausted`, `Aborted` return true. A few lines,
constantly reinvented by callers.

- Custom codes need a retryable flag on registration. `Register`'s signature
  is not yet frozen (pre-v1), so either extend it or add a variadic option.

### 4. Reverse conversion: grpcerr.FromStatus / FromError

Convert a received `*status.Status` (or an error returned by a gRPC call)
back into an `*errtrail.Error`, so clients of other services share the same
Code taxonomy end to end. Map codes 0–16 one-to-one; anything else becomes
`Unknown`.

- If item 1 (errdetails) lands first, also recover the custom code name from
  `ErrorInfo.Reason` when present.

## P2 — hardening and ecosystem

### 5. Thread-safe or frozen code registry

`Register` currently relies on a documented contract ("call before the server
starts"). Enforce it instead: copy-on-write via `atomic.Pointer[map]`, or
freeze the table on first read and panic on late registration. Removes a
whole class of subtle production races.

### 6. Revisit the PublicMessage fallback for gRPC messages

`grpcerr.ToStatus` inherits `PublicMessage`'s `http.StatusText` fallback, so
gRPC clients see HTTP wording ("Internal Server Error") and an empty message
for `Canceled` / custom codes. Consider falling back to the code name on the
gRPC path instead. Behavior change on the wire — needs a minor version bump
and a changelog entry.

### 7. CHANGELOG and a v1.0 plan

Adopters need a signal that the API is stable. Add `CHANGELOG.md` (Keep a
Changelog format), backfill v0.1.x, and state the v1.0 criteria in the
README (essentially: P1 items above settled, no open API questions).

### 8. Coverage reporting in CI

Coverage is already high (core 96.7% / problem 89.5% / grpcerr 100%); make it
visible. Upload `go test -coverprofile` results in CI and add a badge next to
the existing CI badge in the README.

## Explicitly rejected (do not revisit without new evidence)

- **Full stack traces (opt-in or otherwise)** — one frame per wrap is the
  core design pillar; full traces double construction cost and duplicate
  what the trace chain already shows.
- **HTTP middleware / gRPC interceptors** — boundary conversion is two lines
  (`problem.Write` / `grpcerr.ToError`); shipping middleware would drag in
  framework opinions the core deliberately avoids.
- **i18n of public messages** — belongs in the application layer.
