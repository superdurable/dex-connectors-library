# Mailgun connector for Python

Install with `pip install dex-connector-mailgun==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `getEvents` and `listDomains` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getEvents` | `new_get_events_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `listDomains` | `new_list_domains_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |

`getEvents` reads the first page of a sending domain's events (`limit` at most
300); `listDomains` reads one page of domains (`limit` at most 1000, `skip` not
negative) and requires Mailgun's `total_count`. 408, 429, 5xx, and transport
failures retry; other non-200 responses, including 404, select
`providerRejected`.

## Authorization

Create a Dex Web connection named `mailgun` as described in the
[connector README](../README.md#authorization-and-configuration). The private
API key travels only as the HTTP Basic password for user `api` and is resolved
for every attempt.

## Example

[`examples/demo`](examples/demo/flow.py) reads a domain's events and records
them. Run the Worker with `uv run python -m examples.demo.worker` after
`dexcli dev` has loaded a project containing the `mailgun` connection.

## Tests

`mailgunmock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
