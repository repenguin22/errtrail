# Changelog

All notable changes to errtrail, in the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) style.

This repository ships three modules that are versioned **independently** under
[Semantic Versioning](https://semver.org/spec/v2.0.0.html), each with its own
tag line — `vX.Y.Z` (core), `grpcerr/vX.Y.Z`, `otelerr/vX.Y.Z`. They are
therefore listed in separate sections below. This file is the canonical history;
the GitHub Releases pages are a generated view of the same information.

Since v1.0.0 the three modules follow the SemVer compatibility promise — no
breaking change to the public API or to documented wire/response behavior
without a major version bump. See
[Versioning and stability](README.md#versioning-and-stability) in the README.

---

## errtrail (core) — `github.com/repenguin22/errtrail`

### [v1.3.2] — 2026-07-11

Follow-up on the v1.3.1 review-round batch — two spots the sweep still
missed.

- **Fixed** `TestErrorStructSize` (added in v1.3.1 to pin the 144 B layout
  fix) asserted 144 unconditionally, failing on any 32-bit target: `uintptr`
  and slice/string header words are 4 bytes there, and `time.Duration`
  (`retryDelay`) aligns to 4 rather than 8 — hand-computed and confirmed at
  80 B for `GOARCH=386` (no 32-bit CI runner to run it on directly, but
  `go vet`/`go test -c` cross-compile cleanly). The test now branches on
  `unsafe.Sizeof(uintptr(0))`. Test-only; no library behavior changed on
  any platform.
- **Docs** Three more enumerations of the client-visible channels missed
  the retry delay: DESIGN.md's `WithoutPublic` edge-case-table row, the
  README "at the source" intro paragraph, and `detailed`'s (`format.go`)
  internal comment on what `%+v` gathers.

### [v1.3.1] — 2026-07-11

External review round 7 findings.

- **Fixed** `NewSkip` / `WrapSkip` with a `skip` large enough that `3+skip`
  overflows `int` (e.g. `math.MaxInt`) recorded a bogus frame near the top
  of the call stack instead of the documented "unknown" fallback for a skip
  past the stack top. `caller` now guards the overflow explicitly.
- **Changed** `Error`'s allocation drops back to 144 B (was 160 B since
  v1.3.0): `noPublicBelow` moved right after `code` in the struct, filling
  its padding byte instead of adding one at the end. Pure layout — no
  behavior or API change. Pinned by a struct-size test; README/doc.go
  benchmarks and cost notes updated.
- **Changed** `LogValue` no longer costs allocations for public fields and
  field violations it never emits — `collect`'s internal signature gained
  an `includePublic` flag, `false` for `LogValue`, `true` for `%+v`. Same
  chain-walk count, just fewer discarded slices.
- **Docs** The "exactly three channels" wording missed by the v1.3.0 sweep
  — `doc.go`'s package-overview bullet, `DESIGN.md`'s header/struct
  sketch/test-plan lines, and an `inspect_test.go` comment — now says four.

### [v1.3.0] — 2026-07-11

Additive API only — no behavior change for existing code; construction
allocation grows one size class (ROADMAP §5).

- **Added** `(*Error).WithRetryDelay(d)` and `LookupRetryDelay(err)` — the
  **fourth client-visible channel**, carrying a per-error dynamic retry
  delay (a rate limiter's actual time to the next token) that
  `grpcerr.ToStatus` emits as the `RetryInfo` detail, beating the static
  registry delay (`RetryAfter`). Outermost delay wins; blocked below a
  `WithoutPublic` barrier; excluded from `LogValue`; shown as `%+v`'s new
  `public.retry:` line. A non-positive delay is a no-op — the input is a
  computed value and "retry after zero" carries no recommendation
  (deliberately not a panic, unlike registration-time `RetryAfter`). The
  "exactly three channels" contract wording moves to four across the docs.
  `problem` deliberately still emits no `Retry-After` header — the README
  shows the two-line handler pattern via `LookupRetryDelay`.
- **Changed** The `Error` struct grew by the delay field: construction is
  now 160 B (was 144 B), still ~200ns / 1 alloc. README benchmarks updated.

### [v1.2.0] — 2026-07-11

Additive only — no behavior changes for existing code (ROADMAP §4).

- **Added** `NewSkip(skip, code, msg)` and `WrapSkip(skip, err, msg)` —
  caller-skip constructors for error factories (external review round 6). A
  helper like `apperr.NotFound(msg)` built on plain `New` records its own
  line in every trace; `skip=1` points the frame at the factory's caller
  instead (zap `AddCallerSkip` precedent). Separate functions, so the
  `New`/`Wrap` hot path is unchanged. `skip=0` is exactly `New`/`Wrap`; a
  skip past the top of the stack records the `"unknown"` frame; a negative
  skip panics. Core coverage reaches 100% — the over-skip test exercises
  the previously unreachable `caller()` failure branch.

### [v1.1.5] — 2026-07-11

Documentation only — no code change (external review round 6).

- **Docs** External review round 6: `problem.Write` notes that invalid UTF-8
  in public strings is replaced with U+FFFD by encoding/json itself (the HTTP
  boundary tolerates bytes the gRPC transport refuses); DESIGN.md §9 carries
  the poisoned-message caveat recorded in grpcerr's entry below.

### [v1.1.4] — 2026-07-11

A display fix plus review-round docs and tests (Gemini finding 3; external
review round 5).

- **Fixed** `Frame.String()` no longer renders a bogus `" (:0)"` location for
  unresolved frames (both `File` and `Line` zero — e.g. the `"unknown"`
  sentinel a zero-value `Error` resolves to). A frame with either part set
  still prints the location.
- **Docs** External-review (round 5) drift swept: DESIGN.md §9's `RetryDelay`
  spec now carries the v1.1.2 CheckValid gate (delays outside the protobuf
  Duration range are rejected, not saturated); the §6 verb table gains the
  unknown-verb fallback row; `problem.Write` documents that a panicking
  `MarshalJSON` on a public field value propagates (matching encoding/json).
- **Tests** Previously uncovered branches pinned: `%+v` on a nil `*Error`
  (`"<nil>"`), the unknown-verb `%v` fallback, and `Wrapf`'s non-nil path.

### [v1.1.3] — 2026-07-11

Documentation and tests only — no code change (external review on v1.1.2).

- **Docs** The `problem` package comment and `From`'s opening line now name
  all three client-visible channels (they still described the pre-v1.1
  two-channel model), and **`ExampleWrite` teaches the v1.1 pattern**:
  `WithFieldViolation` — which also feeds the gRPC `BadRequest` — instead of
  the old `WithPublicField("errors", ...)` shape.
- **Docs** Remaining v1.1 stragglers: the `Format` verb table lists
  `public.violations`; the internal `walk` contract names violations;
  DESIGN.md records the LogValue exclusion of violations, the explicit-nil
  `"errors"` suppression, the measured construction cost, the full walker
  user list, and a v1.1-current test plan; a README Join sentence now says
  "first in depth-first walk order" instead of the ambiguous "first
  branch's".
- **Docs** The builders' `slices.Clip` comments describe the sharing
  contract precisely: a zero-argument `With()` keeps the same read-only
  backing array — Clip guarantees no shared *appendable capacity*, which is
  what makes later appends safe, not array identity.
- **Tests** The explicit-nil `"errors"` suppression is pinned by a
  regression test (`"errors": null`, derived array suppressed).

### [v1.1.2] — 2026-07-11

Documentation only — no code change (final pre-release review).

- **Docs** DESIGN.md caught up with v1.1 everywhere it still described v0.x:
  Goal 5 now names the three client-visible channels; the `Error` struct
  sketch and the `walk` signature match the implementation (fields,
  violations, barrier flag, `blocked` argument); the `%+v` specification
  includes the `public.violations:` line; the edge-case table states
  `PublicMessage`'s two-level fallback and the per-function `Join` rules;
  `grpcerr.RetryDelay`'s positive-delay contract (v1.1.1) is recorded.
- **Docs** godoc fixes visible on pkg.go.dev: the package overview names the
  three channels and the measured construction cost (~200ns / 144 B /
  1 alloc as of v1.1); `LogValue` documents that field violations are also
  excluded from logs; `problem.From` notes that an explicit
  `WithPublicField("errors", nil)` suppresses the derived member.
- **Docs** `WithoutPublic` misuse warning (godoc + README): call it on a
  fresh `Wrap` — on the error value itself, that node's own public data
  stays above the barrier. A regression test pins both the misuse outcome
  and the documented form.

### [v1.1.1] — 2026-07-11

Documentation only — no code change (review round on v1.1).

- **Docs** Client-visible data is documented as **three channels** —
  `WithPublic`, `WithPublicField`, and `WithFieldViolation` — in the package
  guidelines, the README best practices, and the reclassification warning
  (v1.1 added the third channel but the "only WithPublic/WithPublicField
  reach a client" wording had not caught up). `WithoutPublic`'s contract now
  officially states it blocks all three, including field violations — the
  implementation always did; regression tests pin the barrier at both
  boundaries (`problem` response and gRPC `BadRequest`). DESIGN.md's
  non-goals no longer contradict the v1.1 details support, and the README
  notes that the explicit-`"errors"` override is HTTP-only by design.

### [v1.1.0] — 2026-07-11

Additive only — no behavior changes for existing code (ROADMAP §3).

- **Added** `RetryAfter(d time.Duration)` `RegisterOption` — records a
  recommended retry delay on a custom code and implies the retryable flag (a
  delay is only meaningful for a failure worth retrying). Read back via the
  new **`(Code).RetryDelay() (time.Duration, bool)`**; built-ins,
  unregistered codes, and codes registered without `RetryAfter` report false.
  Panics during `Register` on a non-positive delay.
- **Added** Field violations — a typed channel for validation errors:
  **`FieldViolation{Field, Description}`**, attached via
  **`(*Error).WithFieldViolation(field, description)`** and collected by
  **`FieldViolations(err)`** as a list in walk order (outermost first, Join
  branches depth-first; nothing deduplicated, unlike `PublicFields`).
  Client-visible public data: blocked below a `WithoutPublic` barrier,
  excluded from `LogValue`, shown by `%+v` on a `public.violations:` line.
- **Added** `problem.From` emits field violations as the **`"errors"`
  extension member** (`[{"field", "description"}, ...]`). An explicit
  `WithPublicField("errors", ...)` wins over the derived member; without
  violations the output is unchanged.

### [v1.0.0] — 2026-07-11

- First stable release — identical in code to **v0.7.0**. From here on the
  SemVer compatibility promise applies: no breaking change to the public API
  or to documented wire/response behavior without a major version bump. All
  v1.0 criteria, including the final review-round fixes (v0.7.0), are met;
  the README's [road to v1.0](README.md#the-road-to-v10-record) records them.

### [v0.7.0] — 2026-07-11

The pre-v1.0 semantics fixes from the final review round (2026-07-10, see
ROADMAP §1) —
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

### [grpcerr/v1.3.0] — 2026-07-11

Dynamic retry pushback (ROADMAP §5). Requires core v1.3.0.

- **Changed** The `RetryInfo` detail now prefers the error's own delay
  (`errtrail.WithRetryDelay` — dynamic pushback) over the code's registered
  static `RetryAfter` delay; errors without a per-error delay keep the
  registry behavior exactly. A delay below a `WithoutPublic` barrier is not
  exposed, while the registry delay — code configuration, not error data —
  still applies. Pinned end-to-end over bufconn: the dynamic value reaches
  the client's `RetryDelay` reader. Requires core ≥ v1.3.0.

### [grpcerr/v1.2.0] — 2026-07-11

Frame-placement improvement (ROADMAP §4). Requires core v1.2.0.

- **Changed** `FromError` / `FromStatus` record the frame at their *caller*
  (via core's new `WrapSkip(1)`), so traces start where the wire error
  entered your code. The documented workaround — "the frame points inside
  grpcerr; wrap the result at the call site" — is retired; an existing
  `errtrail.Wrap` at the call site keeps working and simply adds one more
  frame. Requires core ≥ v1.2.0.

### [grpcerr/v1.1.5] — 2026-07-11

Documentation and tests only — no code change (external review round 6).

- **Docs** External review round 6 (measured over bufconn): an invalid-UTF-8
  **public message** sits outside v1.1.4's per-detail isolation — it lives on
  the Status proto itself, so the transport cannot marshal the
  grpc-status-details-bin trailer and the client receives code+message but
  **zero details** (custom-code recovery degrades to the numeric wire code;
  only grpclog sees the failure). `ToStatus` documents the hazard and the
  `utf8.ValidString` guidance; `FromError` warns that the wire message can
  carry raw non-UTF-8 bytes, so echoing it unvalidated into `WithPublic`
  poisons your own response's details. Guarding it in code was considered
  and rejected (ROADMAP): the trailer is the transport's to build, and
  rewriting the message would be silent mutation.
- **Tests** A standing test pins both halves of the mechanism: the
  in-process attach succeeds (3 details) while `proto.Marshal(st.Proto())`
  fails on the poisoned message — so a grpc-go/protobuf change altering
  either half is noticed.

### [grpcerr/v1.1.4] — 2026-07-11

Robustness fix at the wire boundary (backlog item, re-scoped by measurement).

- **Fixed** A detail that proto refuses to marshal no longer takes the other
  details down with it. `ToStatus` attached ErrorInfo/RetryInfo/BadRequest in
  one all-or-nothing batch, so a single poisoned detail — measured: one
  invalid-UTF-8 byte sequence in a `WithFieldViolation` description, the one
  channel that echoes user input — stripped **all** details, silently
  breaking the custom-code round trip (`FromError` degraded to the numeric
  wire code) while the HTTP boundary shipped the same error fine. On batch
  failure each detail is now retried individually, so a poisoned detail
  costs only itself; a detail stays atomic (no partial rewriting of its
  contents), and the status itself is still never lost.

### [grpcerr/v1.1.3] — 2026-07-11

Internal hardening only — no behavior change (external review round 5).

- **Changed** `FromStatus` guards `nil` explicitly instead of relying on
  grpc's undocumented nil-receiver behavior of `Status.Err`/`Code`/`Details`.
  Behavior is unchanged (`nil` still returns `nil`, pinned by an existing
  test); the nil safety is now self-contained.

### [grpcerr/v1.1.2] — 2026-07-11

- **Fixed** `RetryDelay` rejects a `RetryInfo` whose delay is outside the
  protobuf `Duration` range: `AsDuration` silently saturates such a value to
  ±292 years, so a foreign service could make `RetryDelay` report a
  quarter-millennium recommendation as `(d, true)` — a caller sleeping on it
  would hang effectively forever. The delay is now `CheckValid`-gated before
  the positive check; valid inputs are unaffected. errtrail's own
  `RetryAfter` only registers in-range delays, so errtrail-to-errtrail
  traffic was never affected.
- **Tests** A standing adversarial no-leak test serializes the wire proto
  (message + details) of the nastiest realistic chain — credentials in the
  root cause, internal messages/attrs, a std `fmt` layer, a Join with a
  `WithoutPublic` barrier in one branch — and asserts nothing internal
  appears and the blocked branch's violations never become `BadRequest`
  (previously covered only by transient review probes). Core requirement
  unchanged (**v1.1.0**).

### [grpcerr/v1.1.1] — 2026-07-11

- **Fixed** `RetryDelay` treats a `RetryInfo` without a positive delay as
  absent — it returned `(0, true)` for an empty detail (as a foreign service
  may attach), telling callers a recommendation exists when none does. A
  later `RetryInfo` carrying a real delay still wins. errtrail's own
  `RetryAfter` only registers positive delays, so servers using errtrail on
  both ends are unaffected. Core requirement unchanged (**v1.1.0**).

### [grpcerr/v1.1.0] — 2026-07-11

Additive only (ROADMAP §3). Requires core **v1.1.0**.

- **Added** `ToStatus` / `ToError` attach up to three standard details, in
  fixed order: the existing `ErrorInfo` (Domain opt-in), an
  **`errdetails.RetryInfo`** carrying the delay of a code registered with
  `errtrail.RetryAfter`, and an **`errdetails.BadRequest`** built from the
  error's field violations (`errtrail.WithFieldViolation`). RetryInfo and
  BadRequest are independent of `Domain` — registering a delay or attaching
  violations is the opt-in; errors without that data keep the previous wire
  format exactly. `ToStatus`/`ToError` deliberately gain no options: the
  configuration lives in the registry and on the error.
- **Added** `RetryDelay(err) (time.Duration, bool)` — reads the first
  `RetryInfo` off a received status. `FromError` deliberately does not turn
  received details back into public data on the returned error.

### [grpcerr/v1.0.0] — 2026-07-11

- First stable release — identical in code to **grpcerr/v0.5.0** with the core
  requirement bumped to **v1.0.0**. The SemVer compatibility promise applies.

### [grpcerr/v0.5.0] — 2026-07-11

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

### [otelerr/v1.0.1] — 2026-07-11

External review round 7 finding.

- **Fixed** `RecordSpan` no longer lets a `With` attr sharing the reserved
  `"errtrail.code"` key duplicate the taxonomy attribute on the exported
  exception event. A duplicate key's resolution (first-wins, last-wins, or
  an array) varies by tracing backend — an accidental or hostile attr named
  `errtrail.code` could silently corrupt the one attribute alerts and
  dashboards key off of. The attr is now dropped; the real code always
  wins. No core requirement change.

### [otelerr/v1.0.0] — 2026-07-11

- First stable release — identical in code to **otelerr/v0.1.0** (plus the
  server-fault derivation note from the review round) with the core
  requirement bumped to **v1.0.0**. The SemVer compatibility promise applies.

### [otelerr/v0.1.0] — 2026-07-09

- Initial release. `Record(ctx, err)` / `RecordSpan(span, err)` record an error
  on the active OpenTelemetry span (exception event with the code name and
  attrs, span status derived from the code per the OTel gRPC server-span
  conventions), and `TraceAttrs(ctx)` lifts `trace_id`/`span_id` into slog
  attrs. Spans are treated as an internal channel, like logs — the public
  message is never recorded. Requires core **v0.6.0** and
  `go.opentelemetry.io/otel` v1.44.0; Go 1.25+.
