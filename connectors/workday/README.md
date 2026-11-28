# Workday connector

This connector reads worker profiles through Workday's [Common REST Workers API](https://developer.workday.com/documentation/dan1370797991225/). Go v0.2.1, Python and TypeScript v0.2.0 and Java and Rust v0.1.0 implement both `getWorker` and `listWorkers`. All language packages replay the [Go-recorded provider scenarios](../../conformance/connectors/workday/scenarios.json).

`getWorker` accepts the worker's 32-character Workday ID (WID) and returns its `id` and `descriptor` display name. A 404 selects optional `notFound`; 408, 429, 5xx, and transport failures retry. Malformed or oversized JSON selects optional `invalidResponse`. The example leaves optional branches unwired, so they fail the Flow when selected.

`listWorkers` reads one page of workers visible to the integration user. `limit` accepts 1–100 and zero uses Workday's default page size of 20; `offset` is zero based. Optional `search` matches the start of worker names and requires at least three UTF-8 bytes after trimming whitespace. The result includes `total` and a `data` array of worker summaries. Provider rejection and invalid response are optional branches; rate limits, 408, 5xx, and transport failures retry. The Common Workers resource grants reads under the existing security domain; write operations require separate Workday business process APIs and tenant security configuration.

Create an API client for integrations in your Workday tenant. Grant the integration user access to the Worker Data: Public Worker Reports security domain. In Workday **View API Client**, find the tenant's authorization URL, token URL, scopes, and Common REST base URL. Register the exact redirect URI displayed by Dex Web. Enter the Client ID and secret only in the Workday connection; Dex stores access and refresh tokens outside Flow state. Tenant URLs and the necessary scopes vary by deployment, so the connection takes them from the registered client instead of assuming one global endpoint.

The runnable [Go](go/examples/worker-inspection/README.md), [Python](python/examples/worker_inspection/README.md), and [TypeScript](typescript/examples/worker-inspection/README.md) worker inspection Flows demonstrate the Queries, local Dex integration, and FDG 2.0 generation. Live Workday authorization and worker retrieval require tenant access and have not been verified here.

## Java and Rust

The [Java worker](java/examples/io/superdurable/dexconnectors/workday/WorkerDirectoryWorker.java) registers `JavaWorkdayGetWorker` and `JavaWorkdayListWorkers`. Run `./gradlew workerDirectoryWorker` in `java/` with the tenant's `WORKDAY_BASE_URL`, `WORKDAY_TOKEN_ENDPOINT`, and an unexpired `WORKDAY_ACCESS_TOKEN` supplied by your credential manager. This standalone example uses the supplied token for its lifetime. Hosted Java applications bind the named renewable connection with `WorkdayClient.fromProject`. Java SDK 0.1.0 loads hosted project snapshots through the application's S3 object client.

The [Rust lookup](rust/examples/get_worker.rs) and [Rust directory](rust/examples/list_workers.rs) load the `workday` connection with `Client::from_project`. Authorize it in Dex Web and point `DEX_CONNECTOR_CONFIG_FILE` at its owner-only connections file. Run `cargo run --example get_worker` or `cargo run --example list_workers` in `rust/`. The Flow types are `RustWorkdayGetWorker` and `RustWorkdayListWorkers`.

Both workers use `DEX_FLOW_SERVICE_ADDRESS` for the Dex server. Start a lookup with a visible worker's WID in `workerId`, or start a page with `{"limit":2,"offset":5,"search":"Ada"}`. Worker responses contain only WIDs and display names. If Workday echoes the active access token, including in an escaped JSON string, the query selects the `defect` branch before exposing the response to a Flow. The SDK coordinates OAuth refresh for project connections. When Java cannot confirm the outcome of a dispatched refresh-token exchange, it marks that connection for reauthorization to protect a potentially rotated refresh token. A 503 on a worker directory read is retryable in both languages.

Generate the four FDG 2.0 graphs from this directory.

```sh
for flow in java/examples/io/superdurable/dexconnectors/workday/{GetWorkerFlow,ListWorkersFlow}.java rust/examples/{get_worker,list_workers}.rs; do
  dexcli visualize "$flow" --schema-version 2.0 --lint app --open=false --json --out "/tmp/workday-$(basename "$flow").json"
done
```
