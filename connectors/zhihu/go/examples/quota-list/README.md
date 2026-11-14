# Zhihu quota list Flow

This runnable Flow uses the static `zhihu` connection to list quota balances for the Access Secret's account. The `listed` branch stores the typed response in a Dex Attribute and completes with it. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/zhihu/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/quota-list/flow/workflow.go --schema-version 2.0 --json --out ./build/quota-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override zhihu=<artifact-directory>`. In Dex Web **Connections**, create the `zhihu` connection. Sign in to [Zhihu Personal Center](https://developer.zhihu.com/profile) and copy the Data Open Platform Access Secret into the secret `access_secret` field without `Bearer`. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/quota-list
```

In Dex Web **Start Flow**, choose `ZhihuQuotas`, enter a unique Flow ID, and use empty input:

```json
{}
```

The result contains only API category quota balances. The mock-backed integration test uses a real Dex Worker and Client with a synthetic quota and no provider credential.
