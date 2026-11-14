# Tripo account balance Flow

This runnable Flow uses the static `tripo` connection to read available and frozen credits. The `fetched` branch stores the two-field result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/vast/tripo/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/account-balance/flow/workflow.go --schema-version 2.0 --json --out ./build/account-balance
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override tripo=<artifact-directory>`. In Dex Web **Connections**, create the `tripo` connection. Open [Tripo Console API Keys](https://developers.tripo3d.com/en/keys), click **Create Key**, and copy the one-time-shown key into the secret `api_key` field. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/account-balance
```

In Dex Web **Start Flow**, choose `TripoAccountBalance` and enter a unique Flow ID. The start input is an empty object:

```json
{}
```

The output includes `availableCredits` and `frozenCredits` as decimal strings. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
