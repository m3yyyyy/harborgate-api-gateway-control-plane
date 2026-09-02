# Architecture

## Boundary

HarborGate is a control-plane reference implementation. It manages and evaluates desired gateway state, but it does not act as the data plane and cannot proxy a request to an upstream service.

This boundary keeps the demo useful and honest:

- route and policy behavior can be studied;
- approval and audit patterns can be tested;
- no cloud credentials are required;
- no public visitor can use the application to reach an internal network.

## Request flow

1. The React dashboard sends a synthetic request description to `POST /api/v1/simulate`.
2. The API loads the current service and route snapshots from SQLite.
3. The policy evaluator chooses the longest enabled path-prefix match.
4. Method, identity, rate and upstream-health conditions are evaluated.
5. A SHA-256 bucket derived from the request ID and route ID selects the stable or canary variant.
6. The API returns the decision and human-readable reasons. It never opens an upstream connection.

The evaluator is a pure function over an immutable snapshot. This makes decisions deterministic, straightforward to test and independent of HTTP or database concerns.

## Change flow

```text
requested ──► approved ──► applied
    │
    └────────► rejected
```

Only a pending change can be approved or rejected. The approving actor must differ from the requester. Only an approved change can be applied. Applying a proposal validates supported fields, updates the route in one SQLite transaction, increments its revision and records an audit event.

Mutation endpoints require an `X-Admin-Token` value. When `ADMIN_TOKEN` is empty, they fail closed. The dashboard does not embed or request the secret.

## Persistence

SQLite provides an intentionally small local-first database. The process uses one database connection, foreign keys, a busy timeout and write-ahead logging. Schema checks constrain status and policy values.

Audit entries store the previous entry hash. Each current hash covers the previous hash, event ID, action, actor, subject, canonical JSON details and timestamp. `/api/health` and the dashboard recalculate the full chain. This detects modification or reordering, but does not prove authorship because the chain is not digitally signed.

## Failure behavior

- Unknown route: deny.
- Disabled route: excluded from matching.
- Unsupported method: deny.
- Insufficient identity: deny.
- Traffic at or above the configured limit: deny.
- Missing service: deny.
- Offline service: deny.
- Degraded service: allow if other checks pass, with an explicit warning.
- Invalid audit chain: health becomes degraded.
