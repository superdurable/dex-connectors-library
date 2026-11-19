# Personio person inspection Flow

The Worker registers two Flows on the static `personio` connection. `PersonioPersonInspection` calls `getPerson`; `PersonioPersonList` calls `listPersons`. Start the latter with `{"limit":10,"status":"ACTIVE"}` to receive one page of selected IDs and names. Both persist their outcomes in Attributes and expose read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/personio/go`, run:

```bash
dexcli visualize ./examples/person-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/personio-person-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override personio=<artifact-directory>`. In Dex Web Connections, select `personio` and follow the [connection guide](../../../README.md) to create Personio v2 client credentials. Leave `access_token`, `baseUrl`, `timeout`, and `maxResponseBytes` blank.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/personio/go`:

```bash
GOWORK=off go run ./examples/person-inspection
```

Start `PersonioPersonInspection` with a unique Flow ID and input `{"personId":"3003"}`. Replace the sample ID with a person ID you can access. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Real Personio authorization and retrieval remain unverified.
