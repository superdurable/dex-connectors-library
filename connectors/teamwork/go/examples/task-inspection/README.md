# Teamwork task inspection Flow

These runnable Flows call `getTask` and `createTask` through the static `teamwork` connection. They persist task identity and state in Attributes and expose them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/teamwork/go`, run:

```bash
dexcli visualize ./examples/task-inspection/flow --schema-version 2.0 --lint app --json --out /tmp/teamwork-task-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override teamwork=<artifact-directory>`. In Dex Web Connections select the `teamwork` connection. Follow the [connection guide](../../../README.md) to create a Teamwork personal API token and enter it in secret `api_token`. Set required `baseUrl` to the HTTPS root of your Teamwork site; leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/teamwork/go`:

```bash
GOWORK=off go run ./examples/task-inspection
```

Start `TeamworkTaskInspection` with a unique Flow ID and input `{"taskId":"12345"}`. Replace the sample with a task ID returned by Teamwork. The Worker connects to Dex Server without an inbound listener.

Start `TeamworkCreateTask` with a unique Flow ID and input `{"tasklistId":"321","name":"Review launch"}` to create a task in the chosen task list. A possibly effective create is repeated at most three times; an unconfirmed outcome fails on the optional `uncertain` branch. The task list ID comes from the Teamwork Projects task list URL or API.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Teamwork authorization and retrieval remain unverified.
