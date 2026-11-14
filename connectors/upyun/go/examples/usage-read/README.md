# UPYUN storage usage Flow

This runnable Flow calls `getServiceUsage` through the static `upyun` connection, persists the used bytes, and returns a typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/upyun/go`, run:

```bash
dexcli visualize ./examples/usage-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/upyun-usage-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override upyun=<artifact-directory>`. In Dex Web Connections select the `upyun` connection and follow the [connection guide](../../../README.md) to enter `bucket`, `operator`, and secret `password`. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/upyun/go`:

```bash
GOWORK=off go run ./examples/usage-read
```

Start `UPYUNUsageRead` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. CI runs the fixture-backed real-Dex integration test. Real UPYUN authorization and usage remain unverified.
