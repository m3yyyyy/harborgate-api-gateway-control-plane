# HarborGate — Local-First API Gateway Control Plane

[![CI](https://github.com/m3yyyyy/harborgate-api-gateway-control-plane/actions/workflows/ci.yml/badge.svg)](https://github.com/m3yyyyy/harborgate-api-gateway-control-plane/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=061013)](https://react.dev/)
[![SQLite](https://img.shields.io/badge/SQLite-local--first-3f9cd8?logo=sqlite&logoColor=white)](https://www.sqlite.org/)

HarborGate is a production-style reference implementation of an API gateway **control plane**. It models services, versioned routes, access requirements, rate limits, canary percentages and approval-gated configuration changes in a local SQLite database.

The request lab evaluates those policies without forwarding traffic. Placeholder `.internal` URLs are never contacted, which makes the public demo safe to explore.

## Why this project exists

Gateway configuration is often difficult to review before it reaches a live data plane. A small route change can expose an endpoint, send traffic to the wrong service or bypass an expected approval. HarborGate makes the decision path visible:

- longest-prefix route matching;
- HTTP method allow-lists;
- public, authenticated and service-token identity modes;
- per-route traffic thresholds;
- deterministic canary assignment;
- fail-closed handling for missing routes and offline services;
- separation between the actor requesting and approving a change;
- revisioned configuration and a SHA-256-linked audit trail.

## Architecture

```text
React dashboard
      │ same-origin JSON
      ▼
Go HTTP control-plane API
      ├── policy evaluator ──► allow / deny trace
      ├── change workflow ───► pending / approved / applied
      └── SQLite store ──────► services, routes, audit hash chain

No reverse proxy and no connection to placeholder upstream services
```

The distinction is deliberate: HarborGate demonstrates how a control plane can validate and govern desired state. It is not presented as a production reverse proxy. See [architecture](docs/architecture.md) and the [threat model](docs/threat-model.md).

## Run locally on Windows

Requirements: Go 1.24+ and Node.js 22+.

```powershell
git clone https://github.com/m3yyyyy/harborgate-api-gateway-control-plane.git
Set-Location .\harborgate-api-gateway-control-plane

Set-Location .\web
npm.cmd install
npm.cmd run build
Set-Location ..

go run .\cmd\harborgate
```

Open `http://localhost:8080`. The health endpoint is `http://localhost:8080/api/health`.

## Run locally in Linux or WSL

```bash
git clone https://github.com/m3yyyyy/harborgate-api-gateway-control-plane.git
cd harborgate-api-gateway-control-plane/web
npm install
npm run build
cd ..
go run ./cmd/harborgate
```

Then open `http://localhost:8080` from Windows or Linux.

## Verify the project

```powershell
go test .\...
Set-Location .\web
npm.cmd run check
npm.cmd run build
```

Linux and macOS use `go test ./...` and `npm run ...`.

## Explore the policy API

Evaluate an allowed identity request:

```powershell
$body = @{
  method = "GET"
  path = "/identity/users/42"
  identity = "authenticated"
  requestsInLastMinute = 18
  requestId = "demo-identity-42"
} | ConvertTo-Json

Invoke-RestMethod -Method Post `
  -Uri http://localhost:8080/api/v1/simulate `
  -ContentType application/json `
  -Body $body
```

The decision is deterministic for the same route revision and request ID. Try changing the identity to `anonymous`, the method to `POST`, or the request count to `120` to observe a fail-closed decision.

## Approval-gated changes

Mutation endpoints are disabled unless `ADMIN_TOKEN` is set. The browser dashboard never receives this secret.

```powershell
$env:ADMIN_TOKEN = "replace-with-a-long-random-value"
go run .\cmd\harborgate
```

Approve the seeded request using an actor other than `release-bot`:

```powershell
$headers = @{ "X-Admin-Token" = $env:ADMIN_TOKEN }
$approval = @{ actor = "platform-reviewer"; decision = "approved" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Headers $headers -ContentType application/json `
  -Uri http://localhost:8080/api/v1/changes/chg-1042/decision -Body $approval

$apply = @{ actor = "config-controller" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Headers $headers -ContentType application/json `
  -Uri http://localhost:8080/api/v1/changes/chg-1042/apply -Body $apply
```

Self-approval, invalid transitions and unsupported proposal values are rejected. Applying the change increments the route revision and appends a linked audit event.

## API surface

| Method | Path | Purpose | State change |
|---|---|---|---|
| `GET` | `/api/health` | Database and audit-integrity health | No |
| `GET` | `/api/v1/dashboard` | Services, routes, changes and audit evidence | No |
| `POST` | `/api/v1/simulate` | Evaluate one synthetic request | No |
| `POST` | `/api/v1/changes/{id}/decision` | Approve or reject a pending proposal | Yes; token required |
| `POST` | `/api/v1/changes/{id}/apply` | Apply an approved proposal | Yes; token required |

Requests are size limited, unknown JSON fields are rejected and mutation credentials are compared using fixed-length hashes.

## Deployment

`render.yaml` defines a Docker web service with a generated admin token and `/api/health` health check. The demo uses an ephemeral SQLite file, so state may reset after a restart or new deployment. That is intentional for a public portfolio demo.

The container runs as a non-root user. Build it locally with:

```powershell
docker build -t harborgate:local .
docker run --rm -p 8080:8080 harborgate:local
```

## Repository map

```text
cmd/harborgate/      Process startup and graceful shutdown
internal/api/        HTTP contracts, security headers and static UI serving
internal/gateway/    Domain model and deterministic policy evaluator
internal/store/      SQLite migrations, workflow transitions and audit chain
web/                 React and TypeScript dashboard
docs/                Architecture, demo guide and threat model
.github/workflows/   CI build and verification
```

## Portfolio talking point

> Built a local-first API gateway control-plane reference implementation in Go, React and SQLite. It evaluates route, identity, rate-limit, health and canary policies without forwarding real traffic; governs configuration changes through separation of duties; and records revisioned, SHA-256-linked audit evidence. Shipped automated tests, Docker packaging, a Render blueprint and cross-layer security controls.

## License

[MIT](LICENSE)
