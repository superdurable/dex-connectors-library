# SiliconFlow model catalog Flow

This runnable Flow uses the static `siliconflow` connection to list model IDs
available to its API key. The `listed` branch stores the result in a Dex
Attribute and completes with the models. Optional branches are unwired and
fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/siliconflow/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/model-catalog/flow/workflow.go --schema-version 2.0 --json --out ./build/model-catalog
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to
`dexcli dev --connector-release-override siliconflow=<artifact-directory>`.
In Dex Web **Connections**, create the `siliconflow` connection with a secret
API Key from [SiliconFlow API 密钥](https://cloud.siliconflow.cn/account/ak).
Leave the base URL, timeout, and response-size fields blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/model-catalog
```

In Dex Web **Start Flow**, choose `SiliconFlowModelCatalog`, provide a unique
Flow ID, and use empty JSON input:

```json
{}
```

The output contains `models`. A successful empty list means the provider
returned no entries. The mock-backed integration test uses a real Dex Worker
and Client without a SiliconFlow credential. Mock IDs are sample data.
