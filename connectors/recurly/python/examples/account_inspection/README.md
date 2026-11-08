# Recurly Python account inspection

This runnable Flow calls `getAccount` through the static `recurly` connection, persists the selected account identity, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/recurly/python`, run `uv run python -m examples.account_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `RecurlyAccountInspectionPython` with a unique Flow ID and input `{"accountId":"e28zov4fw0v2"}`. Replace the sample account ID with one returned by Recurly.
