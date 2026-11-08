# Buffer channel lookup Flow

This runnable Flow reads one Buffer channel with `getChannel` through the static `buffer` connection and persists its profile. Configure secret `apiKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

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
