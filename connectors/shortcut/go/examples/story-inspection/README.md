# Shortcut story inspection Flow

This runnable Flow calls `getStory` through the static `shortcut` connection. It saves the selected metadata in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/shortcut/go`, run:

```bash
dexcli visualize ./examples/story-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/shortcut-story-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override shortcut=<artifact-directory>`. In Dex Web Connections select the `shortcut` connection. Create an API token at [Account Settings > API Tokens](https://app.shortcut.com/settings/account/api-tokens) and enter it in the secret `api_token` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/shortcut/go`:

```bash
GOWORK=off go run ./examples/story-inspection
```

Start `ShortcutStoryInspection` with a unique Flow ID and input `{"storyId":"12345"}`. Replace the sample ID with a Story ID from a Shortcut Story URL. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
