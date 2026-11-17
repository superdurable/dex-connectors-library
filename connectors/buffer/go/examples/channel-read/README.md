# Buffer channel lookup Flow

This runnable Flow reads one Buffer channel with `getChannel` through the static `buffer` connection and persists its profile. Configure secret `apiKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

The same Worker also registers `BufferIdeaCreate`. Start it with an organization ID from `listOrganizations`, a title, and text, for example `{"organizationId":"org-1","title":"A launch idea","text":"Draft the announcement"}`. It calls `createIdea` and returns the created idea. The example integration test runs this path with the Buffer mock through a real Dex Worker and Client.

The Worker also registers `BufferScheduledPostCreate`. Supply a channel ID, text, and RFC 3339 UTC time, for example `{"channelId":"channel-1","text":"A launch post","dueAt":"2030-01-01T10:00:00Z"}`. It calls `createScheduledPost` and returns the scheduled post. Its integration test runs with the Buffer mock through a real Dex Worker and Client.

## Strict FDG 2.0 generation

From `connectors/buffer/go`, run:

```bash
dexcli visualize ./examples/channel-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/buffer-channel-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override buffer=<artifact-directory>`. In Dex Web Connections select the `buffer` connection. Follow the [connection guide](../../../README.md) to enter secret `apiKey`; leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/buffer/go`:

```bash
GOWORK=off go run ./examples/channel-read
```

Start `BufferChannelRead` with a unique Flow ID and input `{"channelId":"channel-1"}`. Replace the sample IDs with Buffer channel ID from your Buffer account. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Buffer authorization and lookup remain unverified.
