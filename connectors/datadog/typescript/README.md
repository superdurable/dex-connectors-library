# Datadog TypeScript connector

`@superdurable/connector-datadog` provides the generated `newQueryMetricsStep`
and `newListMonitorsStep` factories, typed inputs and outputs, and a Datadog
provider client. It implements `queryMetrics` and `listMonitors` from the
connector manifest. Each operation resolves both private keys for every call.

`queryMetrics` accepts `from` and `to` in Unix seconds and a nonblank Datadog
metric expression. `listMonitors` accepts a zero-based `page` and `pageSize`;
zero page size selects 100. The success branches are `queried` and `listed`.
Monitor IDs use JavaScript numbers; integers above `2^53 - 1` are supported only
when exactly representable as a number, so use Go for arbitrary 64-bit IDs.
Optional `providerRejected`, `invalidResponse`, and `defect` branches fail a
Flow when unwired. HTTP 408, 409, 429, 5xx, and transport failures retry.

The [metrics lookup example](examples/metrics-lookup/README.md) is the consuming
Flow. It binds the static `datadog` connection, calls `queryMetrics`, and
retains the safe result. Its local integration runner exercises the same Flow
through a real Dex Worker and Client with a loopback Datadog stand-in.
`newProjectConnection` validates the named project's configuration and
credentials before binding the client.

From this directory, run `npm ci`, `npm run check`, and, with a local Dex server
address in `DEX_FLOW_SERVICE_ADDRESS`, `npm run integration:metrics-lookup`.
The integration runner does not use live Datadog credentials. Live Datadog
authorization and provider behavior remain unverified.
