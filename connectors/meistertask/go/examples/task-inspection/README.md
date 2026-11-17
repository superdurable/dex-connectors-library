# MeisterTask task inspection Flow

This runnable example offers `MeisterTaskTaskInspection` with `getTask` and `MeisterTaskTaskListing` with `listTasks` through the static `meistertask` connection. Each Flow persists its provider result in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/meistertask/go`, run:

```bash
dexcli visualize ./examples/task-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/meistertask-task-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override meistertask=<artifact-directory>`. In Dex Web Connections select the `meistertask` connection. Follow the [connection guide](../../../README.md) to create a personal access token and enter it in secret `api_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/meistertask/go`:

```bash
GOWORK=off go run ./examples/task-inspection
```

Start `MeisterTaskTaskInspection` with a unique Flow ID and input `{"taskId":"15"}`. Replace the sample with a task ID returned by MeisterTask. The Worker connects to Dex Server without an inbound listener.

To list tasks, start `MeisterTaskTaskListing` with a unique Flow ID and input `{"page":1,"items":10,"status":"open"}`. Blank `status` includes every status; positive `page` and `items` select one page.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real MeisterTask authorization and retrieval remain unverified.
