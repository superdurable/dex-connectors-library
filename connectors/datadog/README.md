# Datadog connector

After the v0.4.0 release, install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/datadog/go@v0.4.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/datadog`.

The `datadog` Go module exposes typed Dex Steps for Datadog. Its [runnable lookup Flow](go/examples/datadog-lookup/README.md) uses the static connection name `datadog`.

The Python package is [`dex-connector-datadog`](python/README.md), imported as
`dex_connectors.datadog`.

The TypeScript package is [`@superdurable/connector-datadog`](typescript/README.md).
It implements `queryMetrics` and `listMonitors` with the same provider request,
retry, and branch behavior as Go. Its [metrics lookup Flow](typescript/examples/metrics-lookup/README.md)
uses the static `datadog` connection and has a real Dex Worker and Client
integration runner backed by a local provider stand-in. Live Datadog account
authorization and provider behavior have not been verified.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewQueryMetricsStep | GET /api/v1/query | `From`, `To` (Unix seconds), and Datadog `Query` expression | `MetricsQuery.Status` and `Series` with millisecond timestamps in `Pointlist` | `queried` | `providerRejected`, `invalidResponse`, `defect` |
| NewListMonitorsStep | GET /api/v1/monitor | zero-based `Page` and `PageSize` (zero selects 100) | `MonitorsPage.Monitors` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| NewGetMonitorStep | GET /api/v1/monitor/{id} | positive `MonitorID` from Monitors > select a monitor | monitor ID, name, type, and state | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewUpdateMonitorStep | PUT /api/v1/monitor/{id} | positive `MonitorID` and at least one absolute `Name`, `Message`, or `Query` value | updated monitor summary | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| NewDeleteMonitorStep | DELETE /api/v1/monitor/{id} | positive `MonitorID`; does not force dependent resource deletion | deleted monitor ID | `deleted` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`queryMetrics`, `listMonitors`, and `getMonitor` are read-only Queries. `updateMonitor` and `deleteMonitor` are safe-to-repeat Mutations. `Page` and `PageSize` are optional in the
input schema, so `{}` lists the first page of 100 monitors.

## Authorization

Create an API key in [Organization Settings > API Keys](https://app.datadoghq.com/organization-settings/api-keys) and an application key in [Organization Settings > Application Keys](https://app.datadoghq.com/organization-settings/application-keys). Give the application key `timeseries_query`, `monitors_read`, and `monitors_write` permissions. The connector sends the secrets in `DD-API-KEY` and `DD-APPLICATION-KEY` headers. The default API host serves the US site; set `baseUrl` to your regional API host for another Datadog site. Find a monitor ID in **Monitors > Manage Monitors > select monitor**; use its numeric ID, not its name. An omitted `Name`, `Message`, or `Query` in an update keeps that field; an explicitly empty `Message` clears it.

`baseUrl`, `timeout`, and `maxResponseBytes` have manifest defaults. The connection form gives the provider URL, setup path, units, and blank behavior for every field. Secrets are resolved for each call and are never copied into Flow input, Results, Failures, receipts, or logs.

## Failure behavior

| Response | Outcome |
| --- | --- |
| HTTP 200 with a valid provider result | Required success branch |
| HTTP 404 for a named resource | `notFound` when declared, otherwise `providerRejected` |
| Other conclusive 4xx or redirects | `providerRejected` |
| HTTP 408, 409, 429, 5xx, or transport failure | Retry under the default one-hour policy |
| Malformed or oversized response | `invalidResponse` |
| Invalid local input or credentials | `defect` before sending |

HTTP redirects are not followed. Provider error bodies are never copied into failures. Unwired optional branches fail the Flow.

## Mocks

The generated `datadogmock` package installs these default answers:

- `queryMetrics`: one `system.cpu.user` series with a 0.23 point.
- `listMonitors`: monitor 12345 named Meridian API latency.

The [mock-backed example test](go/examples/datadog-lookup/flow/workflow_mock_integration_test.go) runs the metric query and `getMonitor` defaults through a real Dex Worker and Client in CI.

## Official API references

- [https://docs.datadoghq.com/api/latest/metrics/query-timeseries-points/](https://docs.datadoghq.com/api/latest/metrics/query-timeseries-points/)
- [https://docs.datadoghq.com/api/latest/monitors/get-all-monitors/](https://docs.datadoghq.com/api/latest/monitors/get-all-monitors/)
- [https://docs.datadoghq.com/api/latest/monitors/get-a-monitors-details/](https://docs.datadoghq.com/api/latest/monitors/get-a-monitors-details/)
- [https://docs.datadoghq.com/api/latest/monitors/edit-a-monitor/](https://docs.datadoghq.com/api/latest/monitors/edit-a-monitor/)
- [https://docs.datadoghq.com/api/latest/monitors/delete-a-monitor/](https://docs.datadoghq.com/api/latest/monitors/delete-a-monitor/)
- [https://docs.datadoghq.com/api/latest/authentication/](https://docs.datadoghq.com/api/latest/authentication/)
- [https://docs.datadoghq.com/account_management/api-app-keys/](https://docs.datadoghq.com/account_management/api-app-keys/)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.
