# Shumei balance check Flow

This runnable Flow uses the static `shumei` connection to read the current account balance. The `retrieved` branch stores the typed result in a Dex Attribute and completes with it. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/shumei/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/balance-check/flow/workflow.go --schema-version 2.0 --json --out ./build/balance-check
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override shumei=<artifact-directory>`. In Dex Web **Connections**, create the `shumei` connection. Sign in to the [Shumei console](https://console.ishumei.com/); Shumei supplies the accessKey when account service is activated, so contact your account manager if you cannot locate it. Paste it into the secret `access_key` field without JSON quotes. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/balance-check
```

In Dex Web **Start Flow**, choose `ShumeiBalance`, enter a unique Flow ID, and use empty input:

```json
{}
```

The result is the account balance in yuan. The mock-backed integration test uses a real Dex Worker and Client with a synthetic balance and no provider credential.
