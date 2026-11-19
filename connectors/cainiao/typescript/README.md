# Cainiao TypeScript connector

`@superdurable/connector-cainiao` provides the read-only `queryWaybillTrace` Query for one electronic waybill. It returns carrier code, waybill code, and bounded logistics events. The host resolves the TOP AppKey, AppSecret, and merchant SessionKey for each call; no credential enters Flow input or persisted output.

The runnable [waybill trace example](examples/waybill-trace/README.md) binds the static `cainiao` connection. Configure it in Dex Web Connections using the guide in `connector.yaml`. The package replays Go provider scenarios for signed request fields, valid events, provider rejection, malformed data, rate limits, oversized responses, and transport failures. Its integration test runs a real Dex Worker and Client against a local TOP fake. Real Cainiao merchant authorization and waybill data have not been verified.

Install with `npm install @superdurable/connector-cainiao`. `CainiaoClient` validates its router URL and limits during construction. `newProjectConnection` loads the named project connection. Normal Flows use the generated `newQueryWaybillTraceStep` factory; `CainiaoClientOptions` can inject an approved sender or deterministic signing clock for tests.
