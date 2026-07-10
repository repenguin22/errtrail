# Changelog

All notable changes to errtrail, in the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) style.

This repository ships three modules that are versioned **independently** under
[Semantic Versioning](https://semver.org/spec/v2.0.0.html), each with its own
tag line — `vX.Y.Z` (core), `grpcerr/vX.Y.Z`, `otelerr/vX.Y.Z`. They are
therefore listed in separate sections below. This file is the canonical history;
the GitHub Releases pages are a generated view of the same information.

All releases are pre-1.0. See [Versioning and stability](README.md#versioning-and-stability)
in the README for what that means and for the criteria for cutting v1.0.

---

## errtrail (core) — `github.com/repenguin22/errtrail`

### [v0.7.0] — 2026-07-10

The pre-v1.0 semantics fixes from the final review round (see ROADMAP §1) —
changes that would have been breaking after v1.0.

- **Added** `(*Error).WithoutPublic()` — a public-data barrier: the cause chain
  below the node contributes no public message and no public fields
  (`LookupPublicMessage`, `PublicFields`, `problem`, and `grpcerr` all respect
  it). The node's own public data and anything added by an outer wrap still
  apply; the internal message, attrs, and trace are unaffected. For
  reclassifications that must hide the original failure (NotFound →
  PermissionDenied), where the inner public data previously leaked through the
  new response.
- **Changed** `Register` validation tightened (panics at registration):
  httpStatus must be in **[400, 599]** (was [100, 599] — a 2xx/3xx mapping made
  `problem.Write` emit a success response), grpcCode in **[1, 16]** (was 0–16 —
  0 is OK, which made `grpcerr.ToError` return nil and silently drop the
  error), and the name must match the `ErrorInfo.Reason` wire constraints
  `[A-Z][A-Z0-9_]+[A-Z0-9]`, ≤ 63 chars (was any non-empty string). A
  registration that used to pass may now panic at startup.
- **Changed** `PublicFields`: within one `*Error`, the **last** `WithPublicField`
  now wins for a duplicate key (was first) — consistent with calling
  `WithPublic` twice. Outermost-wins across nodes and first-branch-wins across
  `Join` branches are unchanged.
- **Changed** `PublicMessage` falls back to the code name (e.g. `"CANCELED"`)
  when `http.StatusText` has no text for the status — it never returns `""`
  for a non-nil error anymore, consistent with the problem title and the gRPC
  message fallbacks.
- **Docs** `IsRetryable`/`Retryable` documented as a transience hint (replay
  safety — idempotency, retry budgets, pushback — stays with the caller);
  `With`/`WithPublicField` ownership contract (values stored by reference,
  hand over an immutable snapshot); `Register`'s composite reads are race-free
  but not linearizable across a concurrent registration; stale `problem`
  package comment fixed; README Join semantics stated precisely; the
  reclassification bullet now points at `WithoutPublic`. An adversarial test
  asserts the serialized HTTP body never contains internal msg/attrs/trace.

### [v0.6.0] — 2026-07-09

- **Added** `LookupPublicMessage(err) (string, bool)` — the first explicitly-set
  public message with no fallback, for callers that want their own fallback
  policy (grpcerr's code name, an i18n layer's translation).
- **Changed** `PublicMessage` is reimplemented on top of it (behavior unchanged,
  including `PublicMessage(nil) == ""`), and `problem.From` now uses it — the
  detail is the explicit message or empty, identical to before. The `problem`
  test suite passes unchanged, proving HTTP responses are byte-for-byte the same.

### [v0.5.0] — 2026-07-09

- **Changed** The code registry is now thread-safe via copy-on-write behind an
  `atomic.Pointer`. `Register` is safe to call at any time, including
  concurrently with lookups — late registration is no longer a data race.
  Registering from `init` remains the recommended pattern. No API change.

### [v0.4.0] — 2026-07-09

- **Added** `CodeByName(name) (Code, bool)`, the reverse lookup from a code name
  to its `Code` (used by `grpcerr.FromError` to recover a custom code from the
  wire).
- **Changed** `Register` now also panics on a duplicate name (names are the
  `CodeByName` key and must be unique).

### [v0.3.0] — 2026-07-09

- **Added** `IsRetryable(err)` and `(Code).Retryable()`, derived from the code.
  Built-ins `Unavailable`, `DeadlineExceeded`, `ResourceExhausted`, `Aborted`
  are retryable; custom codes opt in with the new `Retryable()` `RegisterOption`.
- **Changed** `Register` gained a variadic `opts ...RegisterOption`. Existing
  positional calls compile and behave unchanged.

### [v0.2.0] — 2026-07-08

- **Added** Public extension fields: `(*Error).WithPublicField(key, value)` and
  `PublicFields(err)`, a client-visible channel kept separate from the
  internal-only `With` attrs. Excluded from `LogValue`; shown by `%+v` on a
  `public.fields:` line.
- **Added** `problem`: `Problem.Instance` and `Problem.Extensions` (flattened to
  the top-level JSON object by a new `MarshalJSON`, dropping reserved/empty
  keys), plus `Option` varargs on `From`/`Write` and `problem.Instance(uri)`.

### [v0.1.2] — 2026-07-08

- **Fixed** `problem.From` no longer emits a blank RFC 9457 `title`; it falls
  back to the code name when `http.StatusText` is empty (e.g. Canceled's 499).
- **Changed** `Register` now validates its arguments (non-empty name,
  httpStatus in `[100, 599]`, grpcCode in `0–16`), panicking at registration
  instead of far away at request time.
- **Docs** Warn about the typed-nil footgun when returning `Wrap(err, …)` from a
  function typed to return `error`. Reserved `LogValue` keys documented.

### [v0.1.1] — 2026-07-08

- **Fixed** `CodeOf` / `PublicMessage` / `Trace` / `Attrs` no longer panic on a
  typed-nil `*Error` held in a non-nil `error` interface; `walk` skips it.
- **Changed** Single-pass chain collection for `%+v` and `LogValue` (was 3–4
  walks). Output unchanged.
- **Project** Added an MIT `LICENSE`, golangci-lint (v2) in CI, and documented
  the Go version requirements.

### [v0.1.0] — 2026-07-07

- Initial release. `Code` as the source of truth (0–16 aligned with gRPC),
  one-frame call-site trails via `New`/`Wrap`, stdlib-only core compatible with
  `errors.Is/As/Unwrap/Join`, internal vs. public message separation,
  `slog.LogValuer`, and the `problem` subpackage for RFC 9457 responses.

---

## errtrail/grpcerr — `github.com/repenguin22/errtrail/grpcerr`

### [grpcerr/v0.5.0] — 2026-07-10

- **Added** `FromError` / `FromStatus` now take `opts ...FromOption`; existing
  calls compile unchanged. The variadic shape had to exist before v1.0 —
  adding it later changes the function type, a breaking change under Go
  API-compat rules. First option: **`TrustedDomain(domains ...string)`** —
  custom-code recovery additionally requires the `ErrorInfo.Domain` to match
  one of the given domains, for clients that also talk to services outside
  their own taxonomy. The default (no option) keeps the name+numeric double
  check and does not inspect the Domain, preserving the zero-config
  "same taxonomy" behavior.
- **Changed** *(wire-visible)* `ToStatus` / `ToError` no longer attach an
  `ErrorInfo` for an **unregistered** code: its Reason (`"CODE(123)"`)
  violates the `ErrorInfo.Reason` spec and cannot round-trip anyway.
  Registered codes (built-ins included) are unchanged.
- **Docs** `FromError` / `FromStatus` carry the same typed-nil warning `Wrap`
  has (they also return `*Error`).
- Requires core **v0.7.0**.

### [grpcerr/v0.4.0] — 2026-07-09

- **Changed** *(wire-visible)* The status message now falls back to the **code
  name** instead of `http.StatusText`, so gRPC clients no longer see HTTP
  wording ("Internal Server Error") or an empty message for Canceled/custom
  codes: `Internal → "INTERNAL"`, `Canceled → "CANCELED"`,
  `RATE_LIMITED → "RATE_LIMITED"`. An explicit public message is unchanged.
  Requires core **v0.6.0**.

### [grpcerr/v0.3.0] — 2026-07-09

- **Added** `FromError(err)` and `FromStatus(st)` — convert a received gRPC error
  back into an `*errtrail.Error`, recovering a custom code from an
  `errdetails.ErrorInfo` whose `Reason` names a locally registered code whose
  gRPC code matches the wire code. Requires core **v0.4.0**.

### [grpcerr/v0.2.0] — 2026-07-08

- **Added** Opt-in `grpcerr.Domain`: when set, `ToStatus`/`ToError` attach an
  `errdetails.ErrorInfo{Reason: code.String(), Domain}` so custom code names
  survive the wire. Default empty leaves the wire format unchanged.
- **Changed** Bumped the core dependency to **v0.1.2**;
  `genproto/googleapis/rpc` becomes a direct dependency.

### [grpcerr/v0.1.1] — 2026-07-08

- **Changed** Bumped the core dependency to **v0.1.1** for the typed-nil `*Error`
  panic fix (`ToStatus`/`ToError` call `CodeOf`/`PublicMessage`). No API change.

### [grpcerr/v0.1.0] — 2026-07-07

- Initial release. `ToStatus` / `ToError` convert an errtrail error to
  `*status.Status`; the only module that depends on `google.golang.org/grpc`.

---

## errtrail/otelerr — `github.com/repenguin22/errtrail/otelerr`

### [otelerr/v0.1.0] — 2026-07-09

- Initial release. `Record(ctx, err)` / `RecordSpan(span, err)` record an error
  on the active OpenTelemetry span (exception event with the code name and
  attrs, span status derived from the code per the OTel gRPC server-span
  conventions), and `TraceAttrs(ctx)` lifts `trace_id`/`span_id` into slog
  attrs. Spans are treated as an internal channel, like logs — the public
  message is never recorded. Requires core **v0.6.0** and
  `go.opentelemetry.io/otel` v1.44.0; Go 1.25+.
