# Microsoft Excel Python connector

Install `dex-connector-microsoft-excel==0.1.0`. The import path is
`dex_connectors.microsoft_excel`. The package requires `dex-connectors-sdk==0.6.0`
and Dex Python SDK 1.28.0 or newer.

The package implements four Graph Excel operations: `getTableRows`, `getValues`,
`updateValues`, and `appendTableRows`. Use the operation-specific
`new_get_table_rows_step`, `new_get_values_step`, `new_update_values_step`, and
`new_append_table_rows_step` factories. The checked-in
[approval decision Flow](examples/approval_decision/flow.py) is the runnable
application example. Its static Dex Web `ConnectionName` is `approvals`.

Configure a Microsoft Entra work or school account using the
[connector authorization guide](../README.md#microsoft-setup). OAuth refresh,
private and shared credential decoding, and project connection binding are
generated from `connector.yaml`. Access and refresh tokens stay in the Dex
connection store; Flow input, Attributes, Results, and receipts contain no
credentials.

`getTableRows` and `getValues` read tables and ranges. `updateValues` writes
literal absolute values and safely retries an ambiguous response.
`appendTableRows` reads existing keys, records a heartbeat before sending one
append, and never sends that append again after a possibly accepted request.
An unresolved earlier append selects `uncertain`. Optional branches left
unwired in a Flow fail that Flow.

To verify locally, run `dexcli dev` with a free Dex port, set
`DEX_FLOW_SERVICE_ADDRESS` to that address, and run
`uv run pytest examples/approval_decision/integration_test.py` from this
directory. The test binds a local Graph stand-in on an OS-assigned port and
runs a real Dex Worker and Client. `uv run pytest tests` runs the Go-recorded
provider replay and generated OAuth refresh agreement. Real Microsoft
workbook reads and writes have not been verified.
