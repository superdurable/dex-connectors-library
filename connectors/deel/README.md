# Deel connector

The Go, Python, and TypeScript `getContract` Query reads a Deel contract's ID, type, and status. Worker, client, and compensation details are excluded from Dex Results.

## Connect

In the [Deel organization](https://app.deel.com), open **More > Developer > Access Tokens > Generate new token**. Create an organization token with `contracts:read`, then copy the opaque secret into the Dex Web connection's `api_token` field. The connector sends it in the `Authorization: Bearer` header. Rotate or delete the token on the Access Tokens page. See [Deel authentication](https://developer.deel.com/api/authentication).

Leave `baseUrl` blank for the Sandbox API at `https://api-staging.letsdeel.com/rest`. For a Production token, set it to `https://api.letsdeel.com/rest`. Sandbox and Production use separate tokens. `timeout` is a per-request Go duration; `maxResponseBytes` is a byte limit. Blank uses the displayed defaults. Find the non-secret ID in Deel Contracts or an API response and pass it as `contractId`; blank is invalid. See [Retrieve a single contract](https://developer.deel.com/api/stable/reference/endpoints/contracts/get-contract).

## Examples and verification

Run the [Go](go/examples/contract-inspection/README.md), [Python](python/examples/contract_inspection/README.md), or [TypeScript](typescript/examples/contract-inspection/README.md) contract inspection Flow with the static `deel` connection. Go-recorded conformance checks requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Deel authorization and contract retrieval remain unverified.
