# Datadog connector for Python

Install with `pip install dex-connector-datadog==0.2.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `queryMetrics` and `listMonitors` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `queryMetrics` | `new_query_metrics_step` | `queried` | `providerRejected`, `invalidResponse`, `defect` |
| `listMonitors` | `new_list_monitors_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |

`queryMetrics` requires a positive `from_seconds`, a later `to_seconds`, and a
non-blank Datadog metric query, and accepts only a response whose `status` is
`ok`. `listMonitors` reads a zero-based `page` with `page_size` defaulting to
100, at most 1000; both fields are optional in the published input schema. 408, 409, 429, 5xx, and transport failures retry; other
non-200 responses select `providerRejected` with authentication,
authorization, not-found, or validation failure kinds.
Metric point numbers retain integer JSON form when exact. A point number that
cannot be represented exactly by Go's float64 output selects `invalidResponse`
and identifies its `series[].pointlist[][]` location in the failure.

## Authorization

Create a Dex Web connection named `datadog` as described in the
[connector README](../README.md#authorization). Set `baseUrl` to your Datadog
site's API host. The API and application keys travel only in `DD-API-KEY` and
`DD-APPLICATION-KEY` and are resolved for every attempt.

## Example

[`examples/datadog_lookup`](examples/datadog_lookup/flow.py) queries a metric
interval and records the series. Run the Worker with
`uv run python -m examples.datadog_lookup.worker` after `dexcli dev` has loaded
a project containing the `datadog` connection.

## Tests

`datadogmock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
