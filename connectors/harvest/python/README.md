# Harvest connector for Python

Install with `pip install dex-connector-harvest==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listProjects` and `listTimeEntries` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listProjects` | `new_list_projects_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `listTimeEntries` | `new_list_time_entries_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |

Both read one cursor page: `per_page` zero means 100, at most 2000. The
returned `next_cursor` comes only from a Harvest `links.next` URL on
`https://api.harvestapp.com/v2` for the same resource; any other next URL is an
`invalidResponse`. 408, 409, 429, 5xx, and transport failures retry; 401, 403,
and other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `harvest` as described in the
[connector README](../README.md#connection). The required decimal `accountId`
is sent in `Harvest-Account-Id`, and the personal access token travels only in
the `Authorization: Bearer` header, resolved for every attempt.

## Example

[`examples/harvest_lookup`](examples/harvest_lookup/flow.py) reads a page of
projects and then recent time entries. Run the Worker with
`uv run python -m examples.harvest_lookup.worker` after `dexcli dev` has loaded
a project containing the `harvest` connection.

## Tests

`harvestmock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
