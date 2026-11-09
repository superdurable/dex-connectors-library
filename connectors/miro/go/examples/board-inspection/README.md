# Miro board inspection Flow

This runnable Flow calls `getBoard` through the static `miro` connection. It saves the selected metadata in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/miro/go`, run:

```bash
dexcli visualize ./examples/board-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/miro-board-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override miro=<artifact-directory>`. In Dex Web Connections select the `miro` connection. Create a Developer team app at [Your apps](https://miro.com/app/settings/user-profile/apps), grant `boards:read`, install the app, and enter the issued token in secret `access_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/miro/go`:

```bash
GOWORK=off go run ./examples/board-inspection
```

Start `MiroBoardInspection` with a unique Flow ID and input `{"boardId":"uXjVOD6LSME="}`. Replace the sample ID with the ID from your Miro board URL. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
