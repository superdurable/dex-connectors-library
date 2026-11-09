# Lever opportunity inspection Flow

This runnable Flow calls `getOpportunity` through the static `lever` connection. It persists the selected opportunity stage in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/lever/go`, run:

```bash
dexcli visualize ./examples/opportunity-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/lever-opportunity-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override lever=<artifact-directory>`. In Dex Web Connections select the `lever` connection. Follow the [connection guide](../../../README.md) to create an API key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/lever/go`:

```bash
GOWORK=off go run ./examples/opportunity-inspection
```

Start `LeverOpportunityInspection` with a unique Flow ID and input `{"opportunityId":"250d8f03-738a-4bba-a671-8a3d73477145"}`. Replace the sample with a opportunity ID returned by Lever. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Lever authorization and retrieval remain unverified.
