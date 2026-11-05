# Snowflake Python statement Flows

`StatementResultFlow` submits a parameterized SQL statement and immediately reads partition zero. Use it for statements expected to finish before the first read; a running statement selects the optional `running` branch, which fails this Flow. `CancelStatementFlow` accepts a previously returned statement handle and sends a cancellation. The Go account-usage example demonstrates durable Timer polling for long-running statements.

The static connection name is `snowflake-warehouse`. In Dex Web **Connections**, configure `snowflake / snowflake-warehouse` with the account identifier, optional warehouse, role, database, and schema. Use either the key-pair authorization guide or the programmatic access token guide shown there. Do not put credentials in the Flow input.

From `connectors/snowflake/python`:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:24101 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.account_usage_summary.main
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:24101 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.account_usage_summary.start snowflake-usage-001 examples/account_usage_summary/sample-input.json
```

For a cancellation, supply `{"statementHandle":"<Snowflake UUID handle>"}` in a separate JSON file and pass `--cancel` to the start command. Generate the strict FDG 2.0 view from the runnable Python Flow source:

```sh
dexcli visualize examples/account_usage_summary/flow.py --schema-version 2.0 --json --out /tmp/snowflake-python-fdg
dexcli visualize examples/account_usage_summary/cancel_flow.py --schema-version 2.0 --json --out /tmp/snowflake-python-cancel-fdg
```

To verify the application through a real Dex Worker and Client, start `dexcli dev -dex-port 24101 -web-port 24102 -open=false -zd-data-dir /tmp/dex-snowflake-python` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:24101 uv run pytest examples/account_usage_summary/integration_test.py`. The test uses an inert programmatic access token and a provider stand-in; real Snowflake authorization and execution require the Dex Web connection above.
