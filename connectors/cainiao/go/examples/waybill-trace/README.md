# Cainiao waybill trace Flow

This runnable Flow calls `queryWaybillTrace` through the static `cainiao` connection, persists the typed trace, and completes with it. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/cainiao/go`, run:

```bash
dexcli visualize ./examples/waybill-trace/flow/workflow.go --schema-version 2.0 --json --out /tmp/cainiao-waybill-trace-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override cainiao=<artifact-directory>`. In Dex Web Connections select `cainiao` and follow the [connection guide](../../../README.md) to enter AppKey, secret AppSecret, and secret SessionKey. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/cainiao/go`:

```bash
GOWORK=off go run ./examples/waybill-trace
```

Start `CainiaoWaybillTrace` with a unique Flow ID and input such as `{"carrierCode":"YTO","waybillCode":"12345"}`; use a qualifying Cainiao electronic waybill for live use. The Worker connects to Dex Server without an inbound listener. The mock test runs through a real Dex Worker and Client. Live TOP authorization and trace data remain unverified.
