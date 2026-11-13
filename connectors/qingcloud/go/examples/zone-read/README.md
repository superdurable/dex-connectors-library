# QingCloud zone discovery Flow

This runnable Flow calls `describeZones` through the static `qingcloud` connection, persists the returned zone page, and completes with a typed result. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/qingcloud/go`, run:

```bash
dexcli visualize ./examples/zone-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/qingcloud-zone-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override qingcloud=<artifact-directory>`. In Dex Web Connections select the `qingcloud` connection and follow the [connection guide](../../../README.md) to enter `accessKeyId` and secret `secretAccessKey`. Leave the other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/qingcloud/go`:

```bash
GOWORK=off go run ./examples/zone-read
```

Start `QingCloudZoneRead` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. CI runs the fixture-backed real-Dex integration test. Real QingCloud authorization and zone discovery remain unverified.
