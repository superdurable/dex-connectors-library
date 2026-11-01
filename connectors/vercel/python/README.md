# Vercel connector for Python

Install with `pip install dex-connector-vercel==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listDeployments` and `getDeployment` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listDeployments` | `new_list_deployments_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getDeployment` | `new_get_deployment_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Both accept an optional `team_id`; blank uses the personal account.
`getDeployment` takes a deployment ID or hostname and needs a `uid` or `id` in
the response. 408, 409, 429, 5xx, and transport failures retry. On
`getDeployment`, HTTP 404 selects `notFound`; other non-200 responses select
`providerRejected` with authentication, authorization, not-found, or
validation failure kinds.

## Authorization

Create a Dex Web connection named `vercel` as described in the
[connector README](../README.md#authorization). The access token travels only
in the `Authorization: Bearer` header and is resolved for every attempt.

## Example

[`examples/vercel_lookup`](examples/vercel_lookup/flow.py) lists deployments
and records them. Run the Worker with
`uv run python -m examples.vercel_lookup.worker` after `dexcli dev` has loaded
a project containing the `vercel` connection.

## Tests

`vercelmock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
