# Roadmap

Prioritized feature work toward production readiness / v1.0, split out of the
2026-07-08 review. Each item is intended to be a separate, self-contained
change (its own branch / PR).

All P1 feature work from the original review has shipped (core v0.2.0–v0.4.0,
grpcerr v0.2.0–v0.3.0), and the OpenTelemetry integration from the second
review (2026-07-09) shipped as the `otelerr` submodule. What remains is
hardening and ecosystem work.

## Hardening and ecosystem

Shipped so far: the code registry became thread-safe (copy-on-write) in core
v0.5.0, and CI now measures per-module coverage, writes it to the job summary,
and gates on a 90% floor (self-contained — no external service or badge). Note
that `problem.TypeURL` and `grpcerr.Domain` deliberately stay on the documented
"set before startup" contract — they are plain package variables with no
partial-write hazard, so the cost/benefit of atomics there is different.

### 1. CHANGELOG and a v1.0 plan

Adopters need a signal that the API is stable. Add `CHANGELOG.md` (Keep a
Changelog format), backfill the v0.x releases, and state the v1.0 criteria
in the README (essentially: the P1 features have all shipped, so what's
left is closing the open questions in this file).

### 2. gRPC wire-level round-trip test (the one real E2E gap)

The grpcerr round-trip tests hand a status object across in-process
(`ToError` → `status.FromError`); they never cross a real gRPC transport.
On the wire, `ErrorInfo` details are proto-serialized into the
`grpc-status-details-bin` header — so detail marshaling over real transport,
and the headline "same taxonomy end to end" flow (server error → wire →
`FromError` recovers the custom code), are currently unverified.

- Add `grpcerr/e2e_test.go`: a real `grpc.Server` + `ClientConn` over
  `bufconn` (in-memory listener bundled with grpc — zero new dependencies,
  no network, no flakes). No protoc: register a dummy unary method via a
  hand-written `grpc.ServiceDesc` with `emptypb`.
- Handler returns `grpcerr.ToError` of a custom-code error with `Domain`
  set and a public message; the client `conn.Invoke`s, then asserts
  `FromError` recovers the custom code, the public message, and
  `IsRetryable` — across the actual transport.
- Runs in the existing grpcerr CI leg; ~100–150 lines, no new job.

Deliberately not doing: a real-server HTTP E2E (httptest.NewRecorder already
verifies everything errtrail touches — the transport layer isn't ours) and
any Docker/external-service E2E (non-hermetic tests would only make CI
flaky; problem and otelerr already test against the real recorder/SDK).

## Explicitly rejected (do not revisit without new evidence)

- **Full stack traces (opt-in or otherwise)** — one frame per wrap is the
  core design pillar; full traces double construction cost and duplicate
  what the trace chain already shows.
- **HTTP middleware / gRPC interceptors** — boundary conversion is two lines
  (`problem.Write` / `grpcerr.ToError`); shipping middleware would drag in
  framework opinions the core deliberately avoids.
- **i18n of public messages** — belongs in the application layer.
