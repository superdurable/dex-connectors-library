# Bmob class object count Flow

This runnable Flow uses the static `bmob` connection to count objects in one existing class. The `counted` branch stores the count in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/bmob/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/class-object-count/flow/workflow.go --schema-version 2.0 --json --out ./build/class-object-count
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override bmob=<artifact-directory>`. In Dex Web **Connections**, create the `bmob` connection. In your [Bmob console](https://www.bmobapp.com/), open **设置 > 应用密钥** for the app and copy its Application ID and REST API Key into the two secret fields. Open **控制台 > 应用设置 > 域名管理 > API域名** and enter that app's HTTPS root URL as `baseUrl`. Leave timeout and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/class-object-count
```

In Dex Web **Start Flow**, choose `BmobClassObjectCount` and enter a unique Flow ID:

```json
{"className":"GameScore"}
```

Use a real class name from the app's **数据** section. The output contains only `count`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
