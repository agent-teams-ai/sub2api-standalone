# Attested request non-entry for bounded consumer retry

Status: reviewed source candidate for a dependency of
[ReviewRouter #1028](https://github.com/777genius/review-router-ai/pull/1028).
No retry, release, deployment or production replay is enabled by this plan.
Inspected native main: `e047373eb3bc223bb460bb8b77c2f02a046b4441`.
The paired Gateway base is `a3dafad640689bf08c614a0e0eea3b4cff605bb8`.

## Outcome and exact scope

Produce one trustworthy finite observation, `admission_storage_conflict`,
when a protected account/connection admission transaction fails with
PostgreSQL SQLSTATE `40001` or `40P01` before provider entry. It is only
eligibility evidence for a fresh consumer request. It never retries an existing
native request or grants a new provider permit.

Preserve the existing native entry CAS, lifetime, receipt, request nonce and
transport ownership. Other database errors, JSON/state errors, missing rows or
interfaces, credentials, invalid input, quota, cancellation, deadlines and
post-entry failures never acquire this reason. Ordinary provider scheduling,
credential refresh, request budgets and existing public error shapes stay
unchanged. Legacy OAuth-specific dispatch seams outside the two common account
and connection paths remain ineligible in this first capability.

## Owned source

- `backend/internal/repository/native_gateway_identity.go`: classify only
  protected SQL acquisition/read errors before their current generic mapping.
- `backend/internal/repository/native_gateway_connection.go`: preserve/classify
  lock-query errors inside the physical reader only for protected dispatch;
  default enrollment/refresh callers retain their current behavior. The shared
  decoder in `native_gateway_oauth_connect.go` retains its default behavior;
  only its protected admission call preserves the typed SQL failure.
- A small private repository helper may share the exact PostgreSQL classifier.
- `backend/internal/service/native_gateway_identity.go` and
  `native_gateway_connection_dispatch.go`: record the finite reason at the
  protected admission failures, including all applicable connection lock calls.
- `backend/internal/service/native_gateway_lifetime.go`: retain the immutable
  finite reason under the existing mutex and forbid later native entry.
- `backend/internal/gatewaytransport/registry.go`: negotiate the private receipt
  capability for the exact admitted reservation and conditionally project the
  reason in a closed receipt. Update the public receipt projection only if its
  existing shared receipt path requires it.
- `backend/internal/gatewaytransport/http.go`: a negotiated finite closed
  rejection with an unwritten response enters the existing 202 receipt path.
  Legacy output and already written output are unchanged. Without this bridge,
  an empty default 200 would make Gateway fence unknown before reading the proof.
- Existing lifetime/transport/repository fixtures and this plan.

No schema migration, scheduler, retry loop, provider transport fallback, new
runtime flag, credential route or after-expiry proof is added in native.

## Reviewed invariants

1. The classifier accepts only the typed PostgreSQL error codes `40001` and
   `40P01` from the protected SQL operation. It never examines error messages or
   treats arbitrary errors/timeouts as transient.
2. The lifetime records the reason only while admitted, active, unentered and
   unsealed. Recording it prevents a later `gatewayNativeEntered` transition;
   it precedes finish/seal and cannot overwrite another observation.
3. An eligible receipt still requires positive physical closure: context done,
   forwarding returned, known body, body closed, no close failure, and no entry.
   HTTP EOF and a generic error are not those facts.
4. New native requests the exact private capability in the existing authenticated
   admission callback. Only a matching singleton acknowledgement on a valid
   successful original permit binds it to that reservation. Old Gateway means no
   acknowledgement and the original JSON shape.
5. The finite reason remains absent from legacy lifetime JSON. Only an eligible,
   negotiated reservation emits the new optional receipt property. Duplicate
   admission/status readback never upgrades an existing reservation.
6. Gateway separately persists that negotiation with the original native claim.
   Every Gateway receipt consumer verifies that same stored binding before
   accepting the optional property, including control read and cleanup ACK.
7. The old request is spent. A future consumer successor uses a fresh identity
   and ordinal, an identical body/route, existing budget and current authority.
   This repository implements no consumer successor.

The capability is `account-gateway/native-non-entry/v1`, requested and
acknowledged through a single `X-Gateway-Non-Entry-Capability` header on the
existing private admission exchange. The only initial receipt reason is
`admission_storage_conflict`. Header presence alone is not eligibility.

## Implementation and review sequence

1. Publish this plan and the paired Gateway plan before source changes.
2. Complete independent critique of compatibility, immutable non-entry,
   admission-only SQL classification and both public/private settlement paths.
3. Implement the finite observation and negotiation without changing ordinary
   execution or retry policy; push each coherent commit immediately.
4. Add observable unit/real HTTP cases and actual PostgreSQL failure tests.
5. Run exact-source Go 1.27.4 formatting, focused tests and applicable existing
   native qualification. Join the exact native candidate with the Gateway
   candidate; a fixture pinned to old native cannot qualify this addition.
6. Record exact source/results and keep the dependency unmerged until the producer
   contract, joined qualification and compatibility gates pass. Open it ready for
   review so the existing native CI job (which excludes drafts) can execute; this
   is not a qualification result. No release or production activation here.

## Qualification

- Exact typed SQL conflict qualifies at the protected lock; other errors,
  malformed state and cancellation do not.
- The lifetime cannot enter after recording a reason; concurrent cancellation,
  lease expiry and post-entry failures cannot record it.
- Held/failed physical close prevents eligible receipt emission.
- Old Gateway/new native and new Gateway/old native preserve original messages;
  duplicate/malformed/unacknowledged capability headers cannot enable output.
- Both private control receipts and public terminal settlement use the same
  scoped finite observation.
- Actual PostgreSQL conflict/deadlock fixture observes zero provider requests;
  mock-only code tests are not sufficient qualification.
- Joined tests with lost callback/ACK, restart and wrong reservation identities
  preserve spent claims, physical custody and unknown outcomes.

Use disposable PostgreSQL 17 fixtures and loopback provider transports only.
Current `backend/go.mod` and native workflow pin Go 1.27.4; do not change
dependencies or toolchain to make the tests pass.

### Exact joined qualification

The existing actual-deadlock fixture also exposes
`TestGatewayNativeNonEntryJoinedPostgresProof`. It runs the frozen paired
Gateway `services/account-gateway/test/native-non-entry-joined.ts` through the
same ready/stdin handshake pattern as retained-custody qualification. Both
native and Gateway databases must be new, empty, explicit loopback PG17.10
fixtures. Run once in private mode and once in public mode, each with a new
database pair.

The real protected SQL deadlock flows through native lifetime/receipt, Gateway
original settlement, held durable closure ACK and the public SDK. Before ACK
the proof read refuses; afterwards it proves exactly the spent original. A
duplicate original, canceled new request, missing/wrong identity and expired
authority cannot enter the provider. The Go-side actual provider counter must
remain zero. This qualifies the producer boundary only, not RR/Action successor
execution or the deployment sandbox.

The Go joined test requires `GATEWAY_NATIVE_NON_ENTRY_JOINED_NATIVE_DSN`,
`GATEWAY_NATIVE_NON_ENTRY_JOINED_RUNNER`, its
`GATEWAY_NATIVE_NON_ENTRY_JOINED_RUNNER_SHA256`, the absolute
`GATEWAY_NATIVE_NON_ENTRY_JOINED_NODE` and
`GATEWAY_NATIVE_NON_ENTRY_JOINED_MODE=private|public`.
The child uses `AG_NATIVE_NON_ENTRY_JOINED_DSN` and
`AG_NATIVE_NON_ENTRY_JOINED_ALLOW=disposable_pg17_10`.
No missing variable is replaced with a default database.

## Estimate and limits

Planning estimate, excluding generated files/docs: 180-320 production lines
and 350-650 test lines, 1.5-3 focused engineer-days including real native and
paired Gateway qualification. C/U/N scores are subjective, on 1-10:
complexity 6, utility 6, necessity 9 for the requested proof feature. Utility is
bounded because incident evidence does not identify SQL conflicts as the cause
of every observed rejection. No production reliability improvement is claimed.

## Review and evidence ledger

- Round 1, orchestrator: require durable exact reservation negotiation, latch
  before seal, no historical eligibility backfill, original-worker/closure
  checks and both settlement paths.
- Round 2, independent reviewer: include all receipt consumers; preserve raw
  lock-query classification before the connection reader erases it; inspect all
  protected connection lock call sites. These corrections are included above.
- Implementation review: native classifier/entry latch and shared receipt
  projection approved after duplicate-import and cancelled-child corrections.
- Integration review: negotiated closed non-entry must use existing 202 delivery
  when no output was written; private/public tests now assert this boundary.
- Exact-source runtime execution and joined evidence: pending.

### Source checkpoint and hosted qualification

The plan and two critiques preceded source implementation. They are published
with the first reviewed source checkpoint. Root ran Go 1.27.4 formatting in a
local checkout, but that executor disconnected before any Go test completed;
these published source blobs still need the hosted formatting and test gates.
No native runtime pass is claimed at this checkpoint.

The existing native CI workflow now creates one fresh disposable
`native_non_entry_test_ci` database on its pinned PostgreSQL 17.10 service. It
requires exact top-level PASS evidence and rejects SKIP for the three focused
lifetime tests (with the race detector), the private/public negotiation test,
and the actual protected SQL deadlock test. Existing required checks stay in
place. The joined Gateway test is deliberately a separate exact-candidate gate
and remains pending; the standalone native checks cannot qualify RR/Action
request retry or a production rollout.
