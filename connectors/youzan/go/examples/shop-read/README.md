# Youzan shop read Flow

This runnable Flow calls `getShopBasic` through the static `youzan` connection, persists the shop summary, and completes with its typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/youzan/go`, run:

```bash
dexcli visualize ./examples/shop-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/youzan-shop-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override youzan=<artifact-directory>`. In Dex Web Connections select `youzan` and follow the [connection guide](../../../README.md) to enter a shop `accessToken`. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/youzan/go`:

```bash
GOWORK=off go run ./examples/shop-read
```

Start `YouzanShopRead` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. The mock test runs through a real Dex Worker and Client. Live Youzan authorization and shop data remain unverified.
