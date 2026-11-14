# MiniMax Token Plan usage Flow

This runnable Flow uses the static `minimax` connection to read Token Plan
usage. The `fetched` branch stores the provider result in a Dex Attribute and
completes with the model groups. Optional branches are unwired and fail the
Flow when selected.

## Strict FDG 2.0 generation

From `connectors/minimax/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/token-plan-remains/flow/workflow.go --schema-version 2.0 --json --out ./build/token-plan-remains
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to
`dexcli dev --connector-release-override minimax=<artifact-directory>`.
In Dex Web **Connections**, create the `minimax` connection with a secret
Token Plan API Key from [MiniMax Token Plan](https://platform.minimaxi.com/subscribe/token-plan).
Leave base URL, timeout, and response size blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/token-plan-remains
```

In Dex Web **Start Flow**, choose `MiniMaxTokenPlanRemains`, provide a unique
Flow ID, and use empty JSON input:

```json
{}
```

The output contains `modelGroups` with provider-reported percentages and
window reset times. Mock data is a sample, not a claim about current quota.
The mock-backed integration test uses a real Dex Worker and Client without
a MiniMax credential.
