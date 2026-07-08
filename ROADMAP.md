# Roadmap

Prioritized feature work toward production readiness / v1.0, split out of the
2026-07-08 review. Each item is intended to be a separate, self-contained
change (its own branch / PR).

All P1 feature work from the original review has shipped (core v0.2.0–v0.4.0,
grpcerr v0.2.0–v0.3.0). What remains is hardening and ecosystem work.

## Hardening and ecosystem

### 1. Thread-safe or frozen code registry

`Register` currently relies on a documented contract ("call before the server
starts"). Enforce it instead: copy-on-write via `atomic.Pointer[map]`, or
freeze the table on first read and panic on late registration. Removes a
whole class of subtle production races.

### 2. Revisit the PublicMessage fallback for gRPC messages

`grpcerr.ToStatus` inherits `PublicMessage`'s `http.StatusText` fallback, so
gRPC clients see HTTP wording ("Internal Server Error") and an empty message
for `Canceled` / custom codes. Consider falling back to the code name on the
gRPC path instead. Behavior change on the wire — needs a minor version bump
and a changelog entry.

### 3. CHANGELOG and a v1.0 plan

Adopters need a signal that the API is stable. Add `CHANGELOG.md` (Keep a
Changelog format), backfill the v0.x releases, and state the v1.0 criteria
in the README (essentially: the P1 features have all shipped, so what's
left is closing the open questions in this file).

### 4. Coverage reporting in CI

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
