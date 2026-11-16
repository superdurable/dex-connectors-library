# Notion row-changed example

Every row created or edited in the watched Notion data source starts one `NotionDatabaseRowChangeTypeScript` Flow through the `databaseRowChanged` poll Trigger. The [Flow](flow/workflow.ts) records the delivered change, reads the row back with `newGetPageStep` (properties only), and completes with both. It binds the static Dex connection `notion-row-watcher` and the Trigger binding `database-row-changes`, whose `dataSourcePicker` unit saves the data source ID and name; `getPage`'s optional branches are unwired, so a provider rejection fails the Flow.

The [Worker entrypoint](main.ts) loads the project, runs the Worker, and runs `newProjectDatabaseRowTriggerRunner`, which records each row version in the binding's project Trigger inbox before the cursor moves past it and starts one Flow per Trigger event ID with `newDexFlowTriggerTarget`. The first start records a baseline and starts no Flow for existing rows; a trashed row is never delivered.

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or newer. The analyzer resolves `@superdurable/connector-notion` from `node_modules`, so run it where the package is installed, such as an application that depends on it:

```bash
dexcli visualize ./flow/workflow.ts --schema-version 2.0 --json --out ./build/row_changed
```

1. Create a Notion internal connection as the [connector README](../../../README.md) describes, share the watched database with it, and save its API token in Dex Web **Connections** as `notion-row-watcher`. Its `pollInterval` setting is the wait between polls, at least 5 seconds, and `pollPageSize` the rows per query page.
2. Pick the watched data source for the `database-row-changes` binding in Dex Web: paste the database link from its **•••** menu > **Copy link** at https://app.notion.com and pick a data source, or paste a data source ID from the database's settings > **Manage data sources** > **Copy data source ID**.
3. From the repository root, run `npm ci --prefix connectors/notion/typescript`, start a Dex development server, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address and the Dex project environment for the connection, then run `npm run example:row-changed --prefix connectors/notion/typescript`. Restart it after changing the binding; a changed data source records a new baseline. Under `dexcli dev` the poll cursor stays in process memory, so a restart also records a new baseline.

The deterministic tests are `test/row_changed_workflow.test.ts`, `test/database_row_trigger.test.ts`, and `test/database_row_go_fixtures.test.ts`. With a Dex development server running, `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration --prefix connectors/notion/typescript` runs the example's Flow and target against real Dex and a local stand-in for the Notion data source query: the baseline starts no Flow, each row version starts one Flow, a trashed row starts none, edits made while the Trigger process is stopped arrive after a restart, a 429 pauses the poll for Retry-After, and a stored cursor for another data source records a new baseline without delivering existing rows. Live Notion authorization and data source query behavior require a user-controlled connection and remain unverified.
