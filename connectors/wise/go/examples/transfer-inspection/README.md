# Wise transfer inspection Flow

The Worker registers `WiseTransferInspection` and `WiseProfileInspection` through the static `wise` connection. The profile Flow calls the new `getProfile` operation and stores only profile ID and type. Both Flows expose read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/wise/go`, run:

```bash
dexcli visualize ./examples/transfer-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/wise-transfer-inspection-fdg
dexcli visualize ./examples/transfer-inspection/flow/profile_workflow.go --schema-version 2.0 --lint app --json --out /tmp/wise-profile-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override wise=<artifact-directory>`. In Dex Web Connections select the `wise` connection. Follow the [connection guide](../../../README.md) to create a Wise business personal API token and enter it in secret `api_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Production defaults; select the Sandbox API URL for Sandbox.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/wise/go`:

```bash
GOWORK=off go run ./examples/transfer-inspection
```

Start `WiseTransferInspection` with a unique Flow ID and input `{"transferId":"16521632"}`, or `WiseProfileInspection` with `{"profileId":"101"}`. Replace sample IDs with values returned by Wise. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Wise authorization and retrieval remain unverified.
