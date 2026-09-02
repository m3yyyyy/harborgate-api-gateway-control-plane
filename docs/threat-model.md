# Threat model

## Assets

- route and service configuration;
- approval decisions and actor attribution;
- audit-chain integrity;
- the optional administrator token;
- availability of the policy simulator.

## Trust boundaries

The public browser is untrusted. JSON received by the Go API is untrusted. SQLite is local process state. Placeholder upstream addresses are descriptive data and are never contacted.

## Controls

| Risk | Control |
|---|---|
| Public user changes route state | Mutation endpoints require `ADMIN_TOKEN`; empty configuration disables them |
| Requester approves their own proposal | Case-insensitive actor separation is enforced in the transaction |
| Oversized or ambiguous JSON | Request body is capped at 64 KiB; unknown fields and multiple JSON values are rejected |
| Path-policy confusion | Parsed absolute request paths and segment-aware longest-prefix matching |
| Unbounded traffic input | Numeric simulation input has explicit limits |
| Audit entry modification or reordering | Every entry links to the previous SHA-256 hash; the chain is verified on health reads |
| Partial configuration update | State transition, route revision and audit append share one SQLite transaction |
| Browser injection | React escaping plus restrictive same-origin content security policy |
| Secret leakage to the UI | The admin token stays server-side and is never returned by an API |
| Server-side request forgery | There is no reverse-proxy or outbound HTTP client |

## Residual risks

- Actor names are supplied by the caller and are not an identity-provider assertion.
- SHA-256 chaining is tamper-evident, not a digital signature or external transparency log.
- The single-node SQLite design is not a highly available multi-region control plane.
- Rate-limit evaluation uses a supplied count; HarborGate does not maintain a distributed counter.
- A production deployment would use workload identity, signed change provenance, managed secrets, backups and a separate gateway data plane.

Do not use this reference implementation as an Internet-facing reverse proxy or as the sole authorization layer for a production service.
