# Close connector for Python

Install with `pip install dex-connector-close==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listLeads` and `getLead` with the same branches, defaults,
and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listLeads` | `new_list_leads_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getLead` | `new_get_lead_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listLeads` reads an offset page: `skip` is nonnegative and `limit` zero means
100, at most 100. `getLead` requires a Close lead ID that starts with `lead_`
and selects `notFound` for HTTP 404. 408, 409, 429, 5xx, and transport failures
retry; 401, 403, and other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `close` as described in the
[connector README](../README.md#connection). The API key travels only as the
HTTP Basic username and is resolved for every attempt.

## Example

[`examples/close_lookup`](examples/close_lookup/flow.py) lists a page of leads
and reads the first one. Run the Worker with
`uv run python -m examples.close_lookup.worker` after `dexcli dev` has loaded a
project containing the `close` connection.

## Tests

`closemock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
