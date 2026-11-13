# SendCloud account balance Flow

This runnable Flow uses the static `sendcloud` connection to read the account's balances and daily quota counters. The `retrieved` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/sendcloud/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/account-balance/flow/workflow.go --schema-version 2.0 --json --out ./build/account-balance
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override sendcloud=<artifact-directory>`. In Dex Web **Connections**, create the `sendcloud` connection. Log in at [SendCloud China](https://www.sendcloud.net), then open 发送设置 > API USER, select or create an API_USER, and generate its matching API_KEY. Enter both as secret credentials. Leave base URL, timeout, and response-size fields blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/account-balance
```

In Dex Web **Start Flow**, choose `SendCloudAccountBalance` and enter a unique Flow ID with empty input:

```json
{}
```

The output contains `availableBalance`, `balance`, `dailyQuota`, and `todayUsedQuota`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no SendCloud credential.
