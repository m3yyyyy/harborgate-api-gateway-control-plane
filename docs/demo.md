# Demo guide

## 1. Establish the safety boundary

Open the dashboard and point out **Simulation only** and **No live proxy**. Explain that the `.internal` upstream addresses are never contacted.

## 2. Show an allowed request

Select **Allowed identity lookup** and evaluate it. The matching route permits `GET`, accepts an authenticated identity and remains below its rate limit. The result includes its route revision and stable/canary assignment.

## 3. Show fail-closed behavior

Select **Blocked deployment**. The deployment route requires a service token, so a normal authenticated user is denied. Then select **Rate limit exceeded** to show a different policy reason.

## 4. Explain the configuration model

Use the route table to connect the input to method, identity, limit and canary policy. The evaluator uses the longest matching prefix and excludes disabled routes.

## 5. Explain change governance

The approval queue contains a proposed deployment-route update. `release-bot` requested it, so that actor cannot approve it. Mutation APIs remain locked unless a server-side admin token is configured.

## 6. Close with evidence

The audit timeline shows linked configuration events. The green integrity indicator means HarborGate recalculated the SHA-256 chain successfully. Clarify that hash linking detects tampering but is not a signature.
