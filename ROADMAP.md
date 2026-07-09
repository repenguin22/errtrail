# Roadmap

Prioritized feature work toward production readiness / v1.0, split out of the
2026-07-08 review. Each item is intended to be a separate, self-contained
change (its own branch / PR).

All P1 feature work from the original review has shipped (core v0.2.0–v0.4.0,
grpcerr v0.2.0–v0.3.0). What remains is hardening and ecosystem work, plus one
new feature candidate surfaced by a second review (2026-07-09).

## New modules under consideration

### OpenTelemetry integration (separate `otel` submodule)

Bridge errtrail and OpenTelemetry at the request boundary: set an OTel span's
status from the error's `Code`, record the error on the span, and optionally
lift `trace_id` / `span_id` into the error's attrs for logs. Identified as the
biggest ecosystem gap for real services.

- **Ship it as its own module** (`errtrail/otel`, own `go.mod`), exactly like
  `grpcerr`. OTel is a heavy dependency; keeping it in a separate module means
  the stdlib-only core stays untouched and users who don't use OTel never pull
  it in. This is the whole reason the idea is acceptable — do not add any OTel
  dependency to the core or `problem`.
- Likely surface: a helper that takes a `trace.Span` and an `error` and applies
  `span.SetStatus(...)` derived from `CodeOf(err)` (map retryable/5xx-ish codes
  to `codes.Error`), plus `span.RecordError`. Consider a small helper to pull
  the active span's IDs into `slog.Attr`s for `With`.
- Open questions to settle when designing: the exact `Code` → span status
  mapping; whether to record the internal message or only the code/public data
  on the span (spans are often exported off-box — treat like the public
  channel, not logs); minimum OTel SDK version and its Go floor (mirrors how
  `grpcerr` follows grpc's minimum).
- Follows the same release convention as `grpcerr`: tag the core first, then
  tag `otel/vX.Y.Z`.

## Hardening and ecosystem

Note: the code registry became thread-safe (copy-on-write) in core v0.5.0.
`problem.TypeURL` and `grpcerr.Domain` deliberately stay on the documented
"set before startup" contract — they are plain package variables with no
partial-write hazard, so the cost/benefit of atomics there is different.

### 1. Revisit the PublicMessage fallback for gRPC messages

`grpcerr.ToStatus` inherits `PublicMessage`'s `http.StatusText` fallback, so
gRPC clients see HTTP wording ("Internal Server Error") and an empty message
for `Canceled` / custom codes. Consider falling back to the code name on the
gRPC path instead. Behavior change on the wire — needs a minor version bump
and a changelog entry.

### 2. CHANGELOG and a v1.0 plan

Adopters need a signal that the API is stable. Add `CHANGELOG.md` (Keep a
Changelog format), backfill the v0.x releases, and state the v1.0 criteria
in the README (essentially: the P1 features have all shipped, so what's
left is closing the open questions in this file).

### 3. Coverage reporting in CI

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
