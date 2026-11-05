# Vercel TypeScript connector

Install `@superdurable/connector-vercel@0.1.0` with `@superdurable/connectors-sdk` and `@superdurable/dex`.

`VercelClient` supplies `listDeployments` and `getDeployment` query operations. Bind it with `newConnection`, or load the static `vercel` connection from Dex Web with `newProjectConnection`. The checked-in [lookup Flow](examples/vercel-lookup/README.md) shows a Worker and starter using `newGetDeploymentStep`.

The connection uses a secret Vercel access token from [Account Settings > Tokens](https://vercel.com/account/tokens). It reads `baseUrl`, `timeout`, and `maxResponseBytes` from the project connection. An empty team ID reads the personal account; a nonempty team ID selects a team. Results and receipts never contain the token.

HTTP 408, 409, 429, 5xx, and transport failures retry. HTTP 404 takes `notFound` for `getDeployment`; other conclusive errors take `providerRejected`. Malformed or oversized HTTP 200 responses take `invalidResponse`. Invalid local input or credentials take `defect`.

The conformance suite replays Go-recorded requests and outputs. The integration test runs the checked-in Flow through a real Dex Worker and Client with a local Vercel stand-in. Live Vercel authorization and provider behavior remain unverified.
