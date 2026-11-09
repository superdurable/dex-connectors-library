# ClickHouse TypeScript table inspection

This runnable Flow calls `listTables` through the static `clickhouse` connection and persists the returned table list. Optional branches are unwired and fail the Flow when selected. Configure `baseUrl`, `username`, and secret `password` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/clickhouse/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- analytics`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Replace `analytics` with a database returned by `SHOW DATABASES`.
