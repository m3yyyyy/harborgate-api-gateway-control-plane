# Contributing

Thank you for improving HarborGate.

1. Create a branch from `main`.
2. Keep the control-plane/data-plane boundary explicit.
3. Add or update tests for policy and state-transition behavior.
4. Run `go test ./...`, `npm run check` and `npm run build` in `web`.
5. Open a pull request that explains the operational problem and security impact.

Never add real infrastructure credentials, reachable internal hostnames or a general-purpose outbound proxy. Security reports should follow [SECURITY.md](SECURITY.md).
