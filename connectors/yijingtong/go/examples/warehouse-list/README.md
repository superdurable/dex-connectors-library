# Yijingtong warehouse list Flow

This runnable Flow uses the static `yijingtong` connection to list authorized warehouse metadata. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/yijingtong/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/warehouse-list/flow/workflow.go --schema-version 2.0 --json --out ./build/warehouse-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override yijingtong=<artifact-directory>`. In Dex Web **Connections**, create `yijingtong`. Contact Yijingtong customer service through [its official site](https://www.kjwlxt.com/) to enable API access and obtain your assigned HTTPS production system origin and production token. Enter the origin without a path in `baseUrl` and the token in protected `token`. Both are required. The [official onboarding guide](https://szy.kjwlxt.com/hwc_openapi_docs/?page=overview) warns that the public documentation host is a sandbox. Leave timeout and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/warehouse-list
```

In Dex Web **Start Flow**, choose `YijingtongWarehouseList`, enter a unique Flow ID, and use empty input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic warehouse and no provider credential.
