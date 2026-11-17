# Greenhouse candidate inspection Flow

This runnable Worker exposes `getCandidate` and `listCandidates` Flows through the static `greenhouse` connection. `GreenhouseCandidateList` accepts `ListCandidatesInput{PageSize: 100}` on its first page; pass a returned `NextCursor` as the next start input's `Cursor` to continue pagination. It persists only candidate IDs and names in an Attribute. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/greenhouse/go`, run:

```bash
dexcli visualize ./examples/candidate-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/greenhouse-candidate-inspection-fdg
dexcli visualize ./examples/candidate-inspection/flow/list_candidates.go --schema-version 2.0 --lint app --json --out /tmp/greenhouse-candidate-list-fdg
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
