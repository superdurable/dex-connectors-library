# Dada order status Flow

This runnable Flow calls `queryOrderStatus` through the static `dada` connection, persists the typed status, and completes with it. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/dada/go`, run:

```bash
dexcli visualize ./examples/order-status/flow/workflow.go --schema-version 2.0 --json --out /tmp/dada-order-status-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override dada=<artifact-directory>`. In Dex Web Connections select `dada` and follow the [connection guide](../../../README.md) to enter `appKey`, secret `appSecret`, and `sourceID`. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/dada/go`:

```bash
GOWORK=off go run ./examples/order-status
```

Start `DadaOrderStatus` with a unique Flow ID and input such as `{"orderID":"orderNo1653708880866"}`; use an actual third-party delivery order ID for live use. The Worker connects to Dex Server without an inbound listener. The mock test runs through a real Dex Worker and Client. Live Dada authorization and order status remain unverified.
