# Ashby candidate inspection Flow

This runnable Flow calls `getCandidate` through the static `ashby` connection. It persists the selected candidate profile in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

The same Worker registers `AshbyCandidateRename`, which calls `updateCandidate` with `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e","name":"Updated Candidate"}`. It retains the updated profile in an Attribute. This requires **Candidates write** on the API key; use a disposable candidate for a live run. Its optional branches are unwired.

## Strict FDG 2.0 generation

From `connectors/ashby/go`, run:

```bash
dexcli visualize ./examples/candidate-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-inspection-fdg
dexcli visualize ./examples/candidate-inspection/flow/rename_workflow.go --schema-version 2.0 --json --out /tmp/ashby-candidate-rename-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override ashby=<artifact-directory>`. In Dex Web Connections select the `ashby` connection. Follow the [connection guide](../../../README.md) to create an API key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/ashby/go`:

```bash
GOWORK=off go run ./examples/candidate-inspection
```

Start `AshbyCandidateInspection` with a unique Flow ID and input `{"candidateId":"e9ed20fd-d45f-4aad-8a00-a19bfba0083e"}`. Replace the sample with a candidate ID returned by Ashby. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Ashby authorization, listing, retrieval, and updating remain unverified.
