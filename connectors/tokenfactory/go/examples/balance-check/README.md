# TokenFactory balance check Flow

This runnable Flow uses the static `tokenfactory` connection to read the current account balance and API Key status. The `retrieved` branch stores the typed result in a Dex Attribute and completes with it. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/tokenfactory/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/balance-check/flow/workflow.go --schema-version 2.0 --json --out ./build/balance-check
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override tokenfactory=<artifact-directory>`. In Dex Web **Connections**, create the `tokenfactory` connection. Sign in to the [console](https://console.tokenfactory.cn/), complete real-name verification, and open **API Keys** to create or reveal a Key. Paste it into the secret `api_key` field without `Bearer`. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/balance-check
```

In Dex Web **Start Flow**, choose `TokenFactoryBalance`, enter a unique Flow ID, and use empty input:

```json
{}
```

The result contains the balance in yuan, active status, and `CNY` unit. The mock-backed integration test uses a real Dex Worker and Client with a synthetic balance and no provider credential.
