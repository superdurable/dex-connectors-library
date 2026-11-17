# Microsoft Excel Python connector

Install `dex-connector-microsoft-excel==0.1.1`. The import path is
`dex_connectors.microsoft_excel`. The package requires `dex-connectors-sdk`
0.12.0 or newer below 1, and Dex Python SDK 1.28.0 or newer.

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
literal absolute values, declares `repeatOnUnknown: safe`, and retries every
outcome except a definite refusal. `appendTableRows` reads existing keys before
every send and counts possible sends with a single-dispatch fence. An unknown
outcome retries; the next attempt resends only rows still missing, while
possible sends stay below `call.max_sends` (default 3), and selects `uncertain`
at the cap. Optional branches left
unwired in a Flow fail that Flow.

To verify locally, run `dexcli dev` with a free Dex port, set
`DEX_FLOW_SERVICE_ADDRESS` to that address, and run
`uv run pytest examples/approval_decision/integration_test.py` from this
directory. The test binds a local Graph stand-in on an OS-assigned port and
runs a real Dex Worker and Client. `uv run pytest tests` runs the Go-recorded
provider replay and generated OAuth refresh agreement. Real Microsoft
workbook reads and writes have not been verified.
