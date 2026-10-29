# Datadog connector

The `datadog` Go module exposes typed Dex Steps for Datadog. Its [runnable lookup Flow](examples/datadog-lookup/README.md) uses the static connection name `datadog`.

## Operations

| Factory | Endpoint | Input | Output | Required branch | Optional branches |
| --- | --- | --- | --- | --- | --- |
| NewQueryMetricsStep | GET /api/v1/query | `From`, `To` (Unix seconds), and Datadog `Query` expression | `MetricsQuery.Status` and `Series` with millisecond timestamps in `Pointlist` | `queried` | `providerRejected`, `invalidResponse`, `defect` |
| NewListMonitorsStep | GET /api/v1/monitor | zero-based `Page` and `PageSize` (zero selects 100) | `MonitorsPage.Monitors` | `listed` | `providerRejected`, `invalidResponse`, `defect |

Both operations are read-only Queries.

## Authorization

Create an API key in [Organization Settings > API Keys](https://app.datadoghq.com/organization-settings/api-keys) and an application key in [Organization Settings > Application Keys](https://app.datadoghq.com/organization-settings/application-keys). Give the application key `timeseries_query` and `monitors_read` permissions. The connector sends the secrets in `DD-API-KEY` and `DD-APPLICATION-KEY` headers. The default API host serves the US site; set `baseUrl` to your regional API host for another Datadog site.

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

The [mock-backed example test](examples/datadog-lookup/flow/workflow_mock_integration_test.go) runs the first default through a real Dex Worker and Client in CI.

## Official API references

- [https://docs.datadoghq.com/api/latest/metrics/query-timeseries-points/](https://docs.datadoghq.com/api/latest/metrics/query-timeseries-points/)
- [https://docs.datadoghq.com/api/latest/monitors/get-all-monitors/](https://docs.datadoghq.com/api/latest/monitors/get-all-monitors/)
- [https://docs.datadoghq.com/api/latest/authentication/](https://docs.datadoghq.com/api/latest/authentication/)
- [https://docs.datadoghq.com/account_management/api-app-keys/](https://docs.datadoghq.com/account_management/api-app-keys/)

## Verification limits

The provider calls have not been exercised with live account credentials. Real Dex execution, release compatibility, and provider authorization remain unverified locally. Unlisted response fields and undocumented status details are not asserted.
