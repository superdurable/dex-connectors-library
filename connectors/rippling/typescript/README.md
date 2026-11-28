# Rippling Typescript connector

Implements `getWorker`, returning a worker ID, visible status, and visible title. Go-recorded conformance checks exact requests, branches, retries, and outputs.

In Rippling's Developer app > API credentials, create a token with `workers.read` and save it as `api_token` in Dex Web Connections. See the [connection guide](../README.md) and [runnable example](examples/worker-inspection/README.md). Live provider authorization remains unverified.

The [worker pagination example](examples/worker-inspection/list-flow.ts) reads `listWorkers` through the static `rippling` connection. It returns IDs, statuses, titles and an opaque cursor. Each request uses the configured Rippling base URL.
