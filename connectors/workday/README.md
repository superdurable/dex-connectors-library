# Workday connector

This connector reads a worker profile through Workday's [Common REST Workers API](https://developer.workday.com/documentation/dan1370797991225/). Version `v0.1.0` provides the `getWorker` Query in Go, Python, and TypeScript. The Python and TypeScript packages replay the [Go-recorded provider scenarios](../../conformance/connectors/workday/scenarios.json).

`getWorker` accepts the worker's 32-character Workday ID (WID) and returns its `id` and `descriptor` display name. A 404 selects optional `notFound`; 408, 429, 5xx, and transport failures retry. Malformed or oversized JSON selects optional `invalidResponse`. The example leaves optional branches unwired, so they fail the Flow when selected.

Create an API client for integrations in your Workday tenant. Grant the integration user access to the Worker Data: Public Worker Reports security domain. In Workday **View API Client**, find the tenant's authorization URL, token URL, scopes, and Common REST base URL. Register the exact redirect URI displayed by Dex Web. Enter the Client ID and secret only in the Workday connection; Dex stores access and refresh tokens outside Flow state. Tenant URLs and the necessary scopes vary by deployment, so the connection takes them from the registered client instead of assuming one global endpoint.

The runnable [Go](go/examples/worker-inspection/README.md), [Python](python/examples/worker_inspection/README.md), and [TypeScript](typescript/examples/worker-inspection/README.md) worker inspection Flows demonstrate the Query, local Dex integration, and FDG 2.0 generation. Live Workday authorization and worker retrieval require tenant access and have not been verified here.
