# Rippling connector

The Go, Python, and TypeScript `getWorker` Query reads a Rippling worker ID, visible status, and visible title. Status and title may be empty when the token owner lacks field access. Personal data and compensation details stay out of Dex Results.

## Connect

Follow [Rippling's API token guide](https://developer.rippling.com/documentation/rest-api/essentials/api-tokens): in the Developer app, open **API credentials**, create a token with `workers.read`, and save the opaque secret as `api_token` in Dex Web Connections. Its owner must have permission to view the intended workers.

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Find the non-secret worker ID through [List workers](https://developer.rippling.com/documentation/rest-api/reference/list-workers) and pass it as `workerId`; URL-safe IDs of 1–64 characters are accepted. See [Retrieve a specific worker](https://developer.rippling.com/documentation/rest-api/reference/get-workers).

## Examples and verification

Run the [Go](go/examples/worker-inspection/README.md), [Python](python/examples/worker_inspection/README.md), or [TypeScript](typescript/examples/worker-inspection/README.md) example with the static `rippling` connection. Go-recorded conformance checks requests, branches, retries, and outputs. Real Dex integration tests use fixture providers. Live Rippling authorization and worker retrieval remain unverified.
