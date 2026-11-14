# ZoomEye account quota Flow

This runnable Flow uses the static `zoomeye` connection to read subscription plan and remaining points. The `fetched` branch stores the three-field result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/knownsec/zoomeye/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/account-quota/flow/workflow.go --schema-version 2.0 --json --out ./build/account-quota
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override zoomeye=<artifact-directory>`. In Dex Web **Connections**, create the `zoomeye` connection. Sign in at [ZoomEye Profile](https://www.zoomeye.ai/profile), scroll to **API-KEY**, and copy the key into the secret `api_key` field. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/account-quota
```

In Dex Web **Start Flow**, choose `ZoomEyeAccountQuota` and enter a unique Flow ID. The start input is an empty object:

```json
{}
```

The output includes `plan`, `generalPoints`, and `zoomeyePoints`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
