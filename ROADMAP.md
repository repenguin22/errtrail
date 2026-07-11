# Roadmap

Prioritized feature work toward production readiness / v1.0, split out of the
2026-07-08 review. Each item is intended to be a separate, self-contained
change (its own branch / PR).

**v1.0 shipped 2026-07-11** (core v1.0.0, grpcerr/v1.0.0, otelerr/v1.0.0) —
§1 and §2 below are kept as the record of how it got there; open work lives
under [Post-v1.0 candidates](#post-v10-candidates).

All P1 feature work from the original review has shipped (core v0.2.0–v0.4.0,
grpcerr v0.2.0–v0.3.0), and the OpenTelemetry integration from the second
review (2026-07-09) shipped as the `otelerr` submodule.

## Hardening and ecosystem

Shipped so far: the code registry became thread-safe (copy-on-write) in core
v0.5.0; CI measures per-module coverage and gates on a 90% floor
(self-contained — no external service or badge); and `CHANGELOG.md` +
[Versioning and stability](README.md#versioning-and-stability) now record the
history and the v1.0 criteria. Note that `problem.TypeURL` and `grpcerr.Domain`
deliberately stay on the documented "set before startup" contract — they are
plain package variables with no partial-write hazard, so the cost/benefit of
atomics there is different.

### 1. Pre-v1.0 fixes from the final review round (2026-07-10) — SHIPPED

Shipped as core **v0.7.0** and grpcerr **v0.5.0** (two-stage, core tagged
first as usual; otelerr unchanged). Kept below for the record.

Two external reviews ran before tagging v1.0; every claim below was verified
against the code (the behavioral ones empirically). These are the adopted
items — all semantics changes that would be breaking after v1.0, so they had
to land first.

Code changes:

1. **`(*Error).WithoutPublic()`** — a public-data barrier: the node's cause
   chain below it contributes no public message and no public fields
   (re-adding on the outside still works; internal msg/attrs/trace unaffected).
   Motivation: reclassifying NotFound→PermissionDenied to hide existence today
   leaks the inner "User not found" + `WithPublicField` data through the 403
   (verified). Public *message* can be masked by an outer `WithPublic`;
   public *fields* currently cannot be blocked at all. Implement via a barrier
   flag consulted by `LookupPublicMessage` / `PublicFields` / `collect`.
   Do NOT overload `WithPublic("")` as a sentinel.
2. **Tighten `Register` validation** (panics, like existing checks):
   httpStatus ∈ **[400, 599]** (an error code mapping to 2xx/3xx makes
   `problem.Write` emit success responses); grpcCode ∈ **[1, 16]**
   (grpcCode 0 = OK makes `grpcerr.ToError` return nil — the error vanishes;
   verified); name must match ErrorInfo.Reason constraints
   `[A-Z][A-Z0-9_]+[A-Z0-9]`, ≤ 63 chars (spec quoted in errdetails source;
   all names currently registered in this repo already comply). Rework
   grpcerr's `TestDetailsFallbackOnOKMappedCode` (it registers grpcCode 0);
   keep the WithDetails-failure fallback itself as marshal-failure defense.
3. **`PublicFields`: last-write-wins within one `*Error` node** (currently
   first-wins, verified — inconsistent with `WithPublic` twice = last wins).
   Keep outermost-wins across nodes and first-branch depth-first for Join.
   Implementation: iterate a node's fields in reverse in the walk callback.
4. **`PublicMessage`: second-level fallback to `Code.String()`** when
   `http.StatusText` is empty (Canceled/499, custom non-standard statuses).
   Removes the last path in the library that can hand "" to a client;
   consistent with problem's title and grpcerr's message fallbacks.
5. **grpcerr: add `opts ...FromOption` to `FromError` / `FromStatus` now** —
   adding a variadic parameter later changes the function type and is a
   breaking change under Go API-compat rules, so the option shape must exist
   before v1.0. Implement **`TrustedDomain(domains ...string)`**: when given,
   custom-code recovery additionally requires `ErrorInfo.Domain` to match.
   Default (no option) keeps today's name+numeric double check — do not flip
   the default; it would kill the zero-config "same taxonomy" UX.
6. **grpcerr: don't attach `ErrorInfo` for unregistered codes** — Reason
   "CODE(123)" violates the UPPER_SNAKE spec and can't round-trip anyway.
   Attach only when `errtrail.CodeByName(code.String())` resolves.

Documentation (no behavior change):

- problem package comment still says `PublicMessage`; From uses
  `LookupPublicMessage` (problem/problem.go:1-4). Same staleness in
  `TestFromDetailOmittedWhenEqualTitle`'s comment.
- README Join section: `PublicMessage` takes the first *non-empty* public
  across branches (may come from a different branch than the code — that
  combination is intentional; recommend explicit WithCode+WithPublic on a
  Join whose branches diverge). CodeOf takes the first non-OK.
- `IsRetryable` / `Retryable`: state it is a **transience hint** derived only
  from the Code — replay safety (idempotency, retry budget, pushback) is the
  caller's responsibility. Keep the name (industry-standard term); no rename.
- `With` / `WithPublicField`: ownership contract — values are stored by
  reference (no deep copy); hand over an immutable snapshot and don't mutate
  it afterward (mutating a passed map later changes what problem.Write emits,
  and concurrently is a data race).
- `Register`: note that composite conversions (status + name + retryable read
  separately) are race-free but not linearizable across a concurrent
  registration — one more reason to register at init. No snapshot API needed
  (entries are immutable once registered; the window is one request during a
  late registration).
- `FromError` / `FromStatus`: add the same typed-nil warning `Wrap` has
  (they also return `*Error`; unconditional `return grpcerr.FromError(err)`
  from a function typed `error` is the same footgun).
- otelerr: document that server-fault is derived from `GRPCCode()` and that
  for built-ins this coincides exactly with HTTP 5xx (the six server-fault
  codes are precisely the six 5xx codes); divergence is only possible for
  inconsistently registered custom codes. No RecordHTTP variant needed.
- README reclassification bullet: warn that WithCode does not clear public
  data from below; point at `WithoutPublic()`.

Tests: cover each change above, plus an adversarial "serialized HTTP body
never contains internal msg/attrs/trace" test and TrustedDomain
match/mismatch cases.

Decided against (record, don't re-litigate without new evidence): flipping
FromError's default to no-recovery (kills zero-config UX; numeric+name double
check bounds the blast radius); renaming Retryable→Transient (established
term; docs carry the nuance); a snapshot-consistent registry lookup API;
branch-atomic Join semantics; changing `problem.From(nil)`'s documented
200-OK behavior; deep-copying attr/field values.

### 2. Cut v1.0 — DONE

Tagged core v1.0.0 first, bumped the submodules' core requirement, tagged
`grpcerr/v1.0.0` and `otelerr/v1.0.0`; CHANGELOG entries added and the
README's pre-1.0 wording flipped to the SemVer compatibility promise (no
breaking change without a major bump). The README v1.0 checklist records the
review-round fixes.

Deliberately-not-doing notes kept for the record: a real-server HTTP E2E
(httptest already verifies everything errtrail touches — the transport layer
isn't ours) and any Docker/external-service E2E (non-hermetic tests would only
make CI flaky; problem and otelerr already test against the real
recorder/SDK).

## Post-v1.0 candidates

### 3. grpcerr: automatic RetryInfo / BadRequest details (v1.1) — SHIPPED

Shipped as core **v1.1.0** and grpcerr **v1.1.0** (2026-07-11): the
`RetryAfter` RegisterOption + `(Code).RetryDelay`, the typed field-violation
channel (`WithFieldViolation` / `FieldViolations`, emitted by problem as the
`"errors"` member), automatic `RetryInfo`/`BadRequest` attach in `ToStatus`,
and the client-side `grpcerr.RetryDelay`. Original candidate notes kept below
for the record.

Today further gRPC details are deliberately the caller's job: `ToStatus`
returns the `*status.Status`, so callers chain their own `WithDetails` before
`.Err()` (documented in DESIGN.md §9). Candidate work to make the two common
cases automatic — both purely additive (opt-in), so they fit a minor release
under the SemVer promise:

1. **RetryInfo** — attach `errdetails.RetryInfo` when the resolved code is
   retryable. The registry carries the retryable flag (core v0.3.0) but not a
   delay, which RetryInfo requires; needs a new `RegisterOption` (e.g.
   `RetryAfter(time.Duration)`) — non-breaking, `Register` already takes
   variadic options. Attach only when a delay is registered, so the default
   wire format stays unchanged. Client side: a helper to read the received
   delay could pair with it (`IsRetryable` itself stays code-derived).
2. **BadRequest** — build `errdetails.BadRequest` field violations from
   public fields. Open design question first: mapping generic key/value
   public fields onto `{field, description}` violations is ambiguous, so this
   needs a deliberate convention or a dedicated API (the README's validation
   example — an `"errors"` field holding `{detail, pointer}` entries — is the
   obvious starting shape) rather than guessing from arbitrary fields.
   Design before code.

Candidates only — not scheduled; adopt when a concrete use case shows up.

### 4. Caller-skip constructors (v1.2) — SHIPPED

Shipped as core **v1.2.0** and grpcerr **v1.2.0** (2026-07-11, external
review round 6 proposal): `NewSkip` / `WrapSkip` — skip-aware variants for
error factories (zap `AddCallerSkip` precedent), separate functions so the
`New`/`Wrap` hot path is untouched. `grpcerr.FromError` / `FromStatus` use
`WrapSkip(1)` internally, retiring the documented "wrap at the call site"
workaround. Deliberately only the two: `NewSkipf`/`WrapSkipf` were skipped —
a factory can Sprintf itself; add them only if real demand shows up.

### 5. Per-error dynamic retry delay (v1.3) — SHIPPED

Shipped as core **v1.3.0** and grpcerr **v1.3.0** (2026-07-11; review round
6 proposal F1, adopted once the rate-limiter use case was confirmed):
`WithRetryDelay(d)` on the error lets a rate limiter push the *actual*
time-to-next-token as `RetryInfo`, beating the code-registered static
`RetryAfter`. Read with `LookupRetryDelay` (outermost-wins). It is the
**fourth client-visible channel** — blocked by `WithoutPublic`, excluded
from `LogValue`, shown as `%+v`'s `public.retry:` line; the "exactly three
channels" contract text moved to four everywhere. Non-positive delays are a
no-op (the input is a computed value, and "retry after zero" carries no
recommendation — deliberately not a panic, unlike registration-time
`RetryAfter`). `problem` still emits no `Retry-After` header — the README
shows the two-line handler pattern via `LookupRetryDelay` instead. A
registry delay is code configuration, not error data: it is not blocked by
the barrier.

### 6. Round 7 external review fixes — SHIPPED

Shipped as core **v1.3.1** and otelerr **v1.0.1** (2026-07-11; all 5
confirmed findings, no false positives this round): `caller`'s `3+skip`
integer overflow at `skip` near `math.MaxInt` recorded a bogus frame instead
of the documented "unknown" fallback — now guarded explicitly. `Error`'s
allocation regressed to 160 B when v1.3.0 added `retryDelay`; reordering
`noPublicBelow` to fill `code`'s padding byte (rather than trailing the
struct) recovers the 144 B size class with no behavior change. `LogValue`
was paying allocations for public fields/violations it never emits;
`collect` gained an internal `includePublic` flag. The v1.3.0 doc sweep for
"three channels" -> "four channels" missed a few spots (`doc.go`'s
package-overview bullet, DESIGN.md's header/struct sketch/test-plan lines,
one test comment) — now caught up. `otelerr.RecordSpan` let a `With` attr
literally named `"errtrail.code"` duplicate the reserved taxonomy attribute
on the exported event, which a tracing backend could resolve as
first/last/array depending on implementation — the attr is now dropped so
the real code always wins.

## Explicitly rejected (do not revisit without new evidence)

- **Full stack traces (opt-in or otherwise)** — one frame per wrap is the
  core design pillar; full traces double construction cost and duplicate
  what the trace chain already shows.
- **HTTP middleware / gRPC interceptors** — boundary conversion is two lines
  (`problem.Write` / `grpcerr.ToError`); shipping middleware would drag in
  framework opinions the core deliberately avoids.
- **i18n of public messages** — belongs in the application layer.
- **Merging `ToStatus`'s three chain walks (exporting core's `collect`)** —
  `collect` resolves frames as it walks, so one collect pass costs more than
  the three cheap targeted walks it would replace; boundary code runs once
  per response, where three walks are harmless. The same reasoning covers
  `problem.From`'s four walks and `otelerr.Record`'s three — and `From`
  additionally needs `PublicFields`' outermost-wins merge, which `collect`'s
  raw field list does not provide.
- **Size caps, truncation, or UTF-8 sanitization of gRPC details** — the
  library never silently rewrites client-visible data, and size is the
  transport's budget (metadata limits kill oversized trailers long before
  proto's ~2GB marshal ceiling; a library-side cap would be a guess about
  someone else's deployment). Two failure modes are actually reachable, and
  both stay handled without mutation: a proto-unmarshalable detail (invalid
  UTF-8 echoed into a field violation) is isolated per-detail in
  `withDetails` — a poisoned detail costs only itself; an invalid-UTF-8
  public MESSAGE poisons the Status proto itself, so the transport drops the
  whole details trailer — that one is documented (ToStatus / FromError godoc,
  standing marshal test) rather than guarded, because the trailer is the
  transport's to build and rewriting the message would be the same silent
  mutation this entry rejects.
