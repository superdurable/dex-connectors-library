# Moxin model list Flow

This runnable Flow uses the static `moxin` connection to list models currently available to the platform token and group. The `listed` branch stores the typed response in a Dex Attribute and completes with it. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/moxin/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/model-list/flow/workflow.go --schema-version 2.0 --json --out ./build/model-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override moxin=<artifact-directory>`. In Dex Web **Connections**, create the `moxin` connection. Ask the platform administrator for an account and group, then sign in to the [console](https://api.kokoniworld.com/) and open **令牌管理 → 新建令牌**. Copy the new `sk-...` token into the secret `platform_token` field without `Bearer`. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/model-list
```

In Dex Web **Start Flow**, choose `MoxinModels`, enter a unique Flow ID, and use empty input:

```json
{}
```

The result contains only available model IDs. The mock-backed integration test uses a real Dex Worker and Client with a synthetic model and no provider credential.
