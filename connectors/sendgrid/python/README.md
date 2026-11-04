# SendGrid connector for Python

Install with `pip install dex-connector-sendgrid==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `getStats` and `listTemplates` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getStats` | `new_get_stats_step` | `found` | `providerRejected`, `invalidResponse`, `defect` |
| `listTemplates` | `new_list_templates_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |

`getStats` reads `/v3/stats` for an inclusive `YYYY-MM-DD` range (a blank end
date means today) and sums request and delivered counts across each day's
categories. `listTemplates` reads one page of `/v3/templates` for the
`dynamic` (default) or `legacy` generation with a page size from 1 through 200
(zero means 100), and extracts the next page token from the provider's
next-page URL.

Both operations retry on 408, 429, 5xx, and transport failures; any other
non-200 status selects `providerRejected`.

## Authorization

Create a Dex Web connection named `sendgrid` as described in the
[connector README](../README.md#authorization-and-configuration). The API key
travels only in the `Authorization: Bearer` header and is resolved for every
attempt.

## Example

[`examples/demo`](examples/demo/flow.py) reads global statistics for the
requested range. Run the Worker with `uv run python -m examples.demo.worker`
after `dexcli dev` has loaded a project containing the `sendgrid` connection.

## Tests

`sendgridmock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
