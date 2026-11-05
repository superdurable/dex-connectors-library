# Account usage summary, TypeScript

`AccountUsageSummaryFlow` submits an aggregate over `USAGE_EVENTS` with the
account ID and start date bound outside SQL text. It reads the result once; if
Snowflake reports the statement still running, the Flow asks Snowflake to
cancel it and returns a canceled outcome. The `ConnectionName` is the static
`snowflake-warehouse` connection. Configure it in Dex Web using the
[connector instructions](../../README.md).

From `connectors/snowflake/typescript`, run `npm ci`, start `dexcli dev`, set
`DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and run
`npm run example:account-usage-summary`. Start Flow type
`SnowflakeTypeScriptAccountUsageSummary` with input
`{"accountId":"acct_003","since":"2026-01-01"}`. The Worker loads the
project connection and credentials at startup; neither belongs in Flow input.

For a credential-free check, run
`npm run integration:account-usage-summary` against `dexcli dev`. It uses the
same Flow with generated mocks and checks the completed and canceled paths
through a real Dex Worker and Client.

Generate a strict FDG 2.0 source for review with
`dexcli visualize examples/account-usage-summary/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/snowflake-ts-flow`.
Every Step type has one `dex:group` and `dex:explanation` pair. The current
Dex CLI reports `dynamic_class`, `dynamic_step_target`, and
`unknown_step_target` for generated connector Step factories and their branch
targets, so this strict visualization exits with blocking diagnostics. The
real Worker and Client integration test verifies the same route.
