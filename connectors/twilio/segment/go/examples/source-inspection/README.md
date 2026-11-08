# Segment source inspection Flow

This runnable Flow calls `getSource` through the static `segment` connection. It persists the selected Source identity in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/twilio/segment/go`, run:

```bash
dexcli visualize ./examples/source-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/segment-source-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override segment=<artifact-directory>`. In Dex Web Connections select the `segment` connection. Follow the [connection guide](../../../README.md) to create a Workspace Public API token and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for US defaults; set the EU API URL for an EU Workspace.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/twilio/segment/go`:

```bash
GOWORK=off go run ./examples/source-inspection
```

Start `SegmentSourceInspection` with a unique Flow ID and input `{"sourceId":"source123"}`. Replace the sample with a source ID returned by Segment. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Segment authorization and retrieval remain unverified.
