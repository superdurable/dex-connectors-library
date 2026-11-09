# Greenhouse candidate inspection Flow

This runnable Flow calls `getCandidate` through the static `greenhouse` connection. It persists the selected candidate profile in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/greenhouse/go`, run:

```bash
dexcli visualize ./examples/candidate-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/greenhouse-candidate-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override greenhouse=<artifact-directory>`. In Dex Web Connections, select `greenhouse` and follow the [connection guide](../../../README.md) to create Harvest v3 client credentials. Leave `access_token`, `baseUrl`, `timeout`, and `maxResponseBytes` blank.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/greenhouse/go`:

```bash
GOWORK=off go run ./examples/candidate-inspection
```

Start `GreenhouseCandidateInspection` with a unique Flow ID and input `{"candidateId":53883394}`. Replace the sample ID with a candidate ID you can access. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Real Greenhouse authorization and retrieval remain unverified.
