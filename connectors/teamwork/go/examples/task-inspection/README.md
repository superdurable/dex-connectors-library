# Teamwork task inspection Flow

This runnable Flow calls `getTask` through the static `teamwork` connection. It persists the selected task status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/teamwork/go`, run:

```bash
dexcli visualize ./examples/task-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/teamwork-task-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override teamwork=<artifact-directory>`. In Dex Web Connections select the `teamwork` connection. Follow the [connection guide](../../../README.md) to create a Teamwork business personal API token and enter it in secret `api_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Production defaults; select the Sandbox API URL for Sandbox.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/teamwork/go`:

```bash
GOWORK=off go run ./examples/task-inspection
```

Start `TeamworkTaskInspection` with a unique Flow ID and input `{"taskId":"12345"}`. Replace the sample with a task ID returned by Teamwork. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Teamwork authorization and retrieval remain unverified.
