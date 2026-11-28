# Rippling connector

Go, Python, TypeScript, Java, and Rust implement both worker queries. `getWorker` returns the worker ID, visible title, and visible status. `listWorkers` returns one page of those summaries and a `nextCursor`. Title and status may be empty when the token owner lacks field access. Both queries reject credentials reflected in returned fields. Pagination extracts the cursor from Rippling's link and sends subsequent requests to the configured base URL.

## Connect

Follow [Rippling's API token guide](https://developer.rippling.com/documentation/rest-api/essentials/api-tokens): in the Developer app, open **API credentials**, create a token with `workers.read`, and save the opaque secret as `api_token` in Dex Web Connections. Its owner must have permission to view the intended workers.

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Find the non-secret worker ID through [List workers](https://developer.rippling.com/documentation/rest-api/reference/list-workers) and pass it as `workerId`; URL-safe IDs of 1–64 characters are accepted. See [Retrieve a specific worker](https://developer.rippling.com/documentation/rest-api/reference/get-workers).

For `listWorkers`, set `limit` to 1–100 workers per page, or zero for Rippling's default page size. Pass the previous `nextCursor` as `cursor`; leave it blank for the first page. The opaque cursor is non-secret and must not exceed 2,048 UTF-8 bytes.

## Examples and verification

Run the [Go](go/examples/worker-inspection/README.md), [Python](python/examples/worker_inspection/README.md), or [TypeScript](typescript/examples/worker-inspection/README.md) example with the static `rippling` connection. Go-recorded conformance checks requests, branches, retries, and outputs. Real Dex integration tests use fixture providers. Live Rippling authorization and worker retrieval remain unverified.

The [Java worker](java/examples/io/superdurable/dexconnectors/rippling/WorkerDirectoryWorker.java) registers `JavaRipplingGetWorker` and `JavaRipplingListWorkers`. From `java/`, run `./gradlew workerDirectoryWorker` with `RIPPLING_API_TOKEN` supplied by your credential manager. `RIPPLING_BASE_URL` can override the public HTTPS endpoint. Hosted applications use `RipplingClient.fromProject` with the named connection.

Run the Rust [lookup](rust/examples/get_worker.rs) or [page](rust/examples/list_workers.rs) from `rust/` with `cargo run --example get_worker` or `cargo run --example list_workers`. Set `DEX_CONNECTOR_CONFIG_FILE` to the connections file exported by Dex Web. Each example loads the static `rippling` connection from that file. Set `DEX_FLOW_SERVICE_ADDRESS` to point the examples at your Dex server.

Generate and lint the four Flow Definition Graphs from the connector directory.

```sh
for flow in GetWorkerFlow ListWorkersFlow; do
  dexcli visualize "java/examples/io/superdurable/dexconnectors/rippling/$flow.java" --language java --schema-version 2.0 --lint app --open=false --json --out "/tmp/rippling-$flow"
done
for flow in get_worker list_workers; do
  dexcli visualize "rust/examples/$flow.rs" --language rust --schema-version 2.0 --lint app --open=false --json --out "/tmp/rippling-$flow"
done
```
