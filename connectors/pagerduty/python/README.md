# PagerDuty connector for Python

Install with `pip install dex-connector-pagerduty==0.2.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listIncidents` and `getIncident` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listIncidents` | `new_list_incidents_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getIncident` | `new_get_incident_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listIncidents` sends `limit` (zero means 25, at most 100) and a nonnegative
`offset`. `getIncident` unwraps PagerDuty's `incident` envelope and selects
`notFound` for HTTP 404. 408, 429, 5xx, and transport failures retry; other
non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `pagerduty` as described in the
[connector README](../README.md#authorization). The REST API key travels only
in the `Authorization: Token token=...` header and is resolved for every attempt.

## Example

[`examples/incident_review`](examples/incident_review/flow.py) checks the page
window, lists incidents with `new_list_incidents_step`, and records the page.
Run the Worker with `uv run python -m examples.incident_review.worker` after
`dexcli dev` has loaded a project containing the `pagerduty` connection.

## Tests

`pagerdutymock.Mock` scripts both Queries and defaults to the manifest mock
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
