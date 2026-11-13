# Winit warehouse list Flow

This runnable Flow uses the static `winit` connection to list overseas warehouse metadata for an authorized seller. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/winit/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/warehouse-list/flow/workflow.go --schema-version 2.0 --json --out ./build/warehouse-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override winit=<artifact-directory>`. In Dex Web **Connections**, create `winit`. At [Winit Developer Portal](https://developer.winit.com.cn/document/detail/id/1.html), create a production application under My Applications; enter its Application Code as `platform`, Client ID as protected `client_id`, and Client Secret as protected `client_secret`. Authorize a seller and enter its account name as protected `app_key` and token as protected `token`. The seller can also find the token under Winit Seller > Personal Center > System Settings > Get Token. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/warehouse-list
```

In Dex Web **Start Flow**, choose `WinitWarehouseList`, enter a unique Flow ID, and use empty input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic warehouse and no provider credential.
