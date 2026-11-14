# Pgyer uploaded-app Flow

This runnable Flow uses the static `pgyer` connection to list one page of apps uploaded by the API Key's account. The `listed` branch stores the typed response in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/pgyer/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/app-list/flow/workflow.go --schema-version 2.0 --json --out ./build/app-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override pgyer=<artifact-directory>`. In Dex Web **Connections**, create the `pgyer` connection. Sign in at [Pgyer](https://www.pgyer.com), complete real-name verification, open the account menu → Account Center → [API Info](https://www.pgyer.com/account/api), and enter the API Key in the secret `api_key` field. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/app-list
```

In Dex Web **Start Flow**, choose `PgyerUploadedApps`, enter a unique Flow ID, and use:

```json
{"page":1}
```

The result contains only app and build metadata. The mock-backed integration test uses a real Dex Worker and Client with a synthetic app and no provider credential.
