# ClickHouse Python table inspection

This runnable Flow calls `listTables` through the static `clickhouse` connection, persists the returned table list, and completes. Optional branches are unwired and fail the Flow when selected. Configure `baseUrl`, `username`, and secret `password` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/clickhouse/python`, run `uv run python -m examples.table_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `ClickHouseTableInspectionPython` with a unique Flow ID and input `{"database":"analytics"}`. Replace the sample with a database returned by `SHOW DATABASES`.
