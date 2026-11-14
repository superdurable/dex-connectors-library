# Kuaishou user videos Flow

This runnable Flow calls `listUserVideos` through the static `kuaishou` connection, persists the typed page, and completes with it. Optional branches fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/kuaishou/go`, run:

```bash
dexcli visualize ./examples/user-videos/flow/workflow.go --schema-version 2.0 --json --out /tmp/kuaishou-user-videos-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override kuaishou=<artifact-directory>`. In Dex Web Connections select `kuaishou` and follow the [connection guide](../../../README.md) to enter appID and secret accessToken. Leave other fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/kuaishou/go`:

```bash
GOWORK=off go run ./examples/user-videos
```

Start `KuaishouUserVideos` with a unique Flow ID and input `{"cursor":""}`. Use the previous output's `nextCursor` for another page. The Worker connects to Dex Server without an inbound listener. The mock test runs through a real Dex Worker and Client. Live Kuaishou authorization and video data remain unverified.
