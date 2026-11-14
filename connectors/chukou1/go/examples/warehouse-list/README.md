# Chukou1 warehouse list Flow

This runnable Flow uses the static `chukou1` connection to list overseas warehouse metadata. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/chukou1/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/warehouse-list/flow/workflow.go --schema-version 2.0 --json --out ./build/warehouse-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override chukou1=<artifact-directory>`. In Dex Web **Connections**, create `chukou1`. For a self-developed integration, sign in to the [Chukou1 business system](https://yewu.chukou1.cn/Client/Index.aspx), open Account > Customer Authorization Management, authorize, view the token, and enter it in protected `access_token` without `Bearer`. Leave base URL, timeout, and response size blank for their displayed defaults. Third-party ERP apps must use the separate OAuth2 approval and consent flow.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/warehouse-list
```

In Dex Web **Start Flow**, choose `Chukou1WarehouseList`, enter a unique Flow ID, and use empty input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic warehouse and no provider credential.
