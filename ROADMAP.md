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
v0.5.0; CI measures per-module coverage and gates on a 90% floor
(self-contained — no external service or badge); and `CHANGELOG.md` +
[Versioning and stability](README.md#versioning-and-stability) now record the
history and the v1.0 criteria. Note that `problem.TypeURL` and `grpcerr.Domain`
deliberately stay on the documented "set before startup" contract — they are
plain package variables with no partial-write hazard, so the cost/benefit of
atomics there is different.

### 1. Cut v1.0

**All v1.0 criteria in the README are now met** — the last one closed with
`grpcerr/e2e_test.go`, a bufconn-based wire-level round-trip proving that
`ErrorInfo` details survive a real gRPC transport. What remains is the release
mechanics: tag core v1.0.0 first, bump the submodules' core requirement, then
tag `grpcerr/v1.0.0` and `otelerr/v1.0.0`; add the entries to CHANGELOG.md and
flip the README's pre-1.0 wording to the SemVer compatibility promise (no
breaking change without a major bump).

Deliberately-not-doing notes kept for the record: a real-server HTTP E2E
(httptest already verifies everything errtrail touches — the transport layer
isn't ours) and any Docker/external-service E2E (non-hermetic tests would only
make CI flaky; problem and otelerr already test against the real
recorder/SDK).

## Explicitly rejected (do not revisit without new evidence)

- **Full stack traces (opt-in or otherwise)** — one frame per wrap is the
  core design pillar; full traces double construction cost and duplicate
  what the trace chain already shows.
- **HTTP middleware / gRPC interceptors** — boundary conversion is two lines
  (`problem.Write` / `grpcerr.ToError`); shipping middleware would drag in
  framework opinions the core deliberately avoids.
- **i18n of public messages** — belongs in the application layer.
