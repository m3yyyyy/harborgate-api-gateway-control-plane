# Security policy

## Supported version

Security fixes are applied to the latest commit on `main`.

## Reporting a vulnerability

Please use GitHub's private vulnerability reporting feature instead of opening a public issue. Include the affected endpoint or component, reproduction steps, impact and any suggested mitigation. Do not include real credentials, access tokens or private infrastructure addresses.

## Scope and safe use

HarborGate is a portfolio reference implementation and policy simulator. It does not proxy traffic or connect to the placeholder upstreams in its database. Do not present it as a production gateway.

Keep `ADMIN_TOKEN` secret, use a long random value and rotate it if exposed. The public dashboard does not need this token. See the [threat model](docs/threat-model.md) for controls and residual risks.
