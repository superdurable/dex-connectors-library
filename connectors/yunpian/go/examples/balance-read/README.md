# Yunpian balance lookup Flow

This runnable Flow calls `getAccountBalance` through the static `yunpian` connection, persists the balance, and returns it as a typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/yunpian/go`, run:

```bash
dexcli visualize ./examples/balance-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/yunpian-balance-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override yunpian=<artifact-directory>`. In Dex Web Connections select the `yunpian` connection and follow the [connection guide](../../../README.md) to enter the secret `apiKey`. Leave other fields blank for the displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/yunpian/go`:

```bash
GOWORK=off go run ./examples/balance-read
```

Start `YunpianBalanceRead` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. CI runs the fixture-backed real-Dex integration test. Real Yunpian authorization and lookup remain unverified.
