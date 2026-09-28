# Spotify playlist-tracks example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordPlaylistRequest` persists the typed playlist ID and page request;
2. `ListPlaylistTracks` calls `spotify.NewListPlaylistTracksStep`;
3. `CompletePlaylistTracks` persists the returned page and completes the Flow.

The summary and display RPCs expose the request and page in Dex Web. Only the
`listed` branch is wired. Revoked authorization, access denial, not-found,
provider rejection, invalid response, and defect branches fail the Flow when
selected.

The Flow and application Steps declare stable type names because Dex Web Start
Flow sends the names from the Flow Definition. The start Step implements an
immediate `WaitFor`, which Start Flow invokes before `Execute`.

## Validate the Flow Definition

From `connectors/spotify`, generate strict FDG 2.0:

```bash
mkdir -p build
dexcli visualize ./examples/playlist-tracks/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/playlist-tracks
```

The graph must report `valid: true`. Inside this repository it may warn
`connector_release_required`, because the local connector is not published yet.

## Configure and run

Build release metadata from this exact source and expose it to Dex Web as a
local connector override:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/spotify-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/spotify/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/spotify \
  --version v0.1.0 \
  --tag connectors/spotify/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/spotify-release/connector-release.json \
  --digest-output /tmp/spotify-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/spotify/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override spotify=/tmp/spotify-release
```

Open the Dex Web URL printed by `dexcli`. Under **Connections**, select
`spotify / spotify-playlist-reader`, configure a Spotify OAuth client, and
authorize `playlist-read-private`. Spotify requires the playlist to be owned by
or collaborative with the authenticated user.

In a second terminal, start the Worker with the connection file path shown in
Dex Web:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/spotify"
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/playlist-tracks
```

The Worker listens on `127.0.0.1:8819`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or
`DEX_BLOB_CACHE_DIR` when needed.

In the Run workspace, choose **Start Flow**, select
`SpotifyPlaylistTracks`, choose the Worker, enter a unique Flow ID, and submit:

```json
{
  "playlistId": "3cEYpjA9oz9GiPac4AsH4n",
  "market": "US",
  "limit": 20,
  "offset": 0
}
```

The completed output includes `tracks`, `total`, and `nextOffset`. To load
another page, start another domain-appropriate execution or route another
Connector Step with the returned offset; do not infer the offset from the
number of available tracks because `skippedItems` may be nonzero.

When presenting the result, retain each `spotifyUrl`, link metadata back to
Spotify, and use Spotify's official logo according to its
[Design and Branding Guidelines](https://developer.spotify.com/documentation/design).
Do not download Spotify content or use Spotify content to train an AI model.

## Test

From `connectors/spotify`:

```bash
GOWORK=off go test -race ./examples/playlist-tracks/...
```

The real Dex test uses a deterministic fake Spotify API. It verifies a 429
retry after `Retry-After`, successful typed completion, terminal access denial,
the retained display RPC, and credential redaction:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/playlist-tracks/... -count=1 -v
```
