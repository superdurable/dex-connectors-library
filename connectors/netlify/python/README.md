# Netlify connector for Python

Install with `pip install dex-connector-netlify==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listSites` and `listDeploys` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listSites` | `new_list_sites_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `listDeploys` | `new_list_deploys_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listSites` lists every site the token can see; `listDeploys` requires a site
ID. 408, 409, 429, 5xx, and transport failures retry. On `listDeploys`, HTTP
404 selects `notFound`; other non-200 responses select `providerRejected` with
authentication (401), authorization (403), not-found (404), or validation
(400, 422) failure kinds.

## Authorization

Create a Dex Web connection named `netlify` as described in the
[connector README](../README.md#authorization). The personal access token
travels only in the `Authorization: Bearer` header and is resolved for every
attempt.

## Example

[`examples/netlify_lookup`](examples/netlify_lookup/flow.py) lists sites and
records them. Run the Worker with
`uv run python -m examples.netlify_lookup.worker` after `dexcli dev` has loaded
a project containing the `netlify` connection.

## Tests

`netlifymock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
