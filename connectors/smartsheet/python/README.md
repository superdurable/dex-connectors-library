# Smartsheet connector for Python

Install with `pip install dex-connector-smartsheet==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and
implements the manifest Queries `listSheets` and `getSheet` with the same
branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listSheets` | `new_list_sheets_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getSheet` | `new_get_sheet_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listSheets` reads one page: `page` zero means 1, `page_size` zero means 100,
at most 1000. `getSheet` requires a positive sheet ID and returns its columns
and rows, keeping each row's cell objects as untouched JSON; 404 selects
`notFound`. 408, 409, 429, 5xx, and transport failures retry; 401, 403, and
other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `smartsheet` as described in the
[connector README](../README.md#connection). The API access token travels only
in the `Authorization: Bearer` header and is resolved for every attempt.

## Example

[`examples/smartsheet_lookup`](examples/smartsheet_lookup/flow.py) lists a page
of sheets and reads the first one. Run the Worker with
`uv run python -m examples.smartsheet_lookup.worker` after `dexcli dev` has
loaded a project containing the `smartsheet` connection.

## Tests

`smartsheetmock.Mock` scripts both Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
