# YouTube playlist-curator example

This example runs one Flow from Dex Web **Start Flow**:

1. `RecordCurationRequest` validates the typed start input and persists it in
   the `youtube-curation-request` Attribute;
2. `LookupChannel` calls `youtube.NewGetChannelStep` with the handle or channel
   ID, and `ChannelResolved` records the channel ID;
3. `SearchLatestVideo` calls `youtube.NewSearchVideosStep` for the channel's
   newest video matching the query, and `LatestVideoChosen` picks it or
   completes with `noMatch`;
4. `AddToPlaylist` calls `youtube.NewAddPlaylistItemStep`, which inserts the
   video only when the playlist does not already hold it, and `VideoAdded`
   records the item;
5. `ConfirmPlaylistItem` calls `youtube.NewListPlaylistItemsStep` filtered to
   the video, and `CurationConfirmed` completes with `isConfirmed`.

Every read Step uses the `youtube-readonly` connection (method
`google-oauth-readonly`, scope `youtube.readonly`); only `AddToPlaylist` uses
the `youtube` connection (method `google-oauth-manage`, scope `youtube`). An
unknown channel completes through `ChannelNotFound`. The summary and display
RPCs show the request and the `youtube-curation-outcome` Attribute. Other
branches, such as `quotaExhausted`, are unwired, so they fail the Flow.

## Validate the Flow Definition

From `connectors/google/youtube`:

```bash
mkdir -p build
dexcli visualize ./examples/playlist-curator/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/playlist-curator
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required`, because a local module is not a published
release.

## Configure and run

Dex needs release metadata built from this source, passed as an override. From
the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/youtube-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/google/youtube/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/google/youtube \
  --version v0.1.0 --tag connectors/google/youtube/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/youtube-release/connector-release.json \
  --digest-output /tmp/youtube-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/google/youtube/build" \
  --connector-release-override google-youtube=/tmp/youtube-release
```

Open Dex Web and select **Connections**. Configure two connections, following
each guide: enable the YouTube Data API v3, create a Web OAuth client with the
redirect URI Dex shows, and enter its client ID and secret:

- `google / youtube-readonly` with the **Google OAuth, read-only** method;
- `google / youtube` with the **Google OAuth, manage** method.

Authorize both with the Google account that owns the target playlist.

In a second terminal, start the Worker with the project's `DEX_PROJECT_*`
environment:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/google/youtube"
go run ./examples/playlist-curator
```

The Worker listens on `127.0.0.1:8893`. Override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. It logs the
connection names, never a token.

In the Run workspace, choose **Start Flow**, select `YouTubePlaylistCurator`,
choose the Worker at `127.0.0.1:8893`, enter a Flow ID, and submit:

```json
{
  "channel": "@GoogleDevelopers",
  "query": "release notes",
  "playlistId": "PLyour-playlist-id"
}
```

The run completes with a `CurationOutcome` whose `status` is `added`. Starting
it again with the same input completes with `wasAlreadyPresent: true` and no
second insert. The search costs 100 quota units and the insert 50.

## Test

From `connectors/google/youtube`:

```bash
GOWORK=off go test -race ./examples/playlist-curator/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/playlist-curator/... -count=1
```

The real Dex suite covers an insert that answers HTTP 503 after storing the
item, whose retry finds the item instead of adding it twice, and asserts that reads
send the read-only token and the insert sends the manage token, plus an unknown
handle, no matching video, and invalid start input.
