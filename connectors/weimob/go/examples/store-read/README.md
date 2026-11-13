# Weimob store read Flow

This runnable Flow calls `getStoreBasic` through the static `weimob` connection, persists the store summary, and completes with its typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/weimob/go`, run:

```bash
dexcli visualize ./examples/store-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/weimob-store-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override weimob=<artifact-directory>`. In Dex Web Connections select `weimob` and follow the [connection guide](../../../README.md) to enter `clientID`, secret `clientSecret`, and optional `shopID`. Leave transport fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/weimob/go`:

```bash
GOWORK=off go run ./examples/store-read
```

Start `WeimobStoreRead` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. The mock test runs through a real Dex Worker and Client. Live Weimob authorization and store data remain unverified.
