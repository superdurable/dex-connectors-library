# Datadog metrics lookup

`MetricsLookupFlow` accepts `from`, `to` (Unix seconds), and a Datadog metric
`query`. It calls `newQueryMetricsStep` through the static `datadog` connection
and completes with `MetricsQuery`. The optional failure branches are unwired.

From `connectors/datadog/typescript`, install dependencies with `npm ci`.
Start `dexcli dev` with ports of your choice, set `DEX_FLOW_SERVICE_ADDRESS` to
the resulting Dex gRPC address, and run `npm run integration:metrics-lookup`.
The runner uses a provider stand-in on a free local port and checks a rate-limit
retry, successful completion, and an unwired authentication rejection through
a real Dex Worker and Client.

For a configured Datadog connection in Dex Web, run
`npx tsx examples/metrics-lookup/main.ts`. The Worker reads project
configuration from the environment and keeps the keys outside Flow state.
Start `DatadogMetricsLookup` with a request such as
`{"from":1793145600,"to":1793149200,"query":"system.cpu.user{*}"}`.
Use a recent window for a live account.

Generate and inspect the Flow Definition Graph with
`dexcli visualize examples/metrics-lookup/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/datadog-metrics-lookup`.
