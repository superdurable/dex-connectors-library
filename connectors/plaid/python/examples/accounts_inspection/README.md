# Plaid Python accounts inspection

This runnable Flow calls `getAccounts` through the static `plaid` connection, persists the selected account routing summaries, and completes. Optional branches are unwired and fail the Flow when selected. Configure `client_id`, `secret`, and `access_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/plaid/python`, run `uv run python -m examples.accounts_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `PlaidAccountsInspectionPython` with a unique Flow ID and input `{"itemId":"item_123456"}`. Replace the sample Item ID with one returned by Plaid.
