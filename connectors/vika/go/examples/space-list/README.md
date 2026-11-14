# Vika workspace list Flow

This runnable Flow uses the static `vika` connection to list the workspaces the API Token's user created or joined. The `listed` branch stores the typed response in a Dex Attribute and completes with it. Optional error branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/vika/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/space-list/flow/workflow.go --schema-version 2.0 --json --out ./build/space-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override vika=<artifact-directory>`. In Dex Web **Connections**, create the `vika` connection. Sign in to [Vika](https://vika.cn), open the lower-left profile menu → User Center → Developer Configuration, then click `+` to create an API Token. Bind an email if prompted. Copy the token into the secret `api_token` field without `Bearer`. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/space-list
```

In Dex Web **Start Flow**, choose `VikaSpaces`, enter a unique Flow ID, and use empty input:

```json
{}
```

The result contains only workspace IDs, names, and administrator flags. The mock-backed integration test uses a real Dex Worker and Client with synthetic workspaces and no provider credential.
