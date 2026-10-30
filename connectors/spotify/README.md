# Spotify Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/spotify/go@v0.24.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/spotify`.

The Spotify connector lists one bounded page of tracks from a playlist through
Spotify's official [Get Playlist Items](https://developer.spotify.com/documentation/web-api/reference/get-playlists-items)
endpoint. Spotify currently permits that endpoint only for playlists the
authenticated user owns or collaborates on; other playlists return
`accessDenied`.

`listPlaylistTracks` is a Query. It accepts a 22-character playlist ID, an
optional market, a limit from 1 through 50, and a zero-based offset. Zero limit
uses Spotify's default of 20. The result contains available tracks in playlist
order, the provider page metadata, and `nextOffset` when another page follows.
Unavailable or unexpected non-track items are omitted from `tracks` and counted
in `skippedItems`, so the next request must use `nextOffset` rather than the
number of returned tracks.

The connector requests only `playlist-read-private` through Spotify OAuth with
PKCE. The access token is resolved immediately before each provider call and
never enters Flow input, Results, receipts, logs, fixtures, or generated code.
Credentials stay in project storage. When Spotify's one-hour access token is
within five minutes of its recorded expiry, the connector refreshes it before
the call and project storage atomically preserves or replaces the returned
refresh token. Spotify [refresh tokens now expire after six
months](https://developer.spotify.com/documentation/web-api/tutorials/refreshing-tokens);
`invalid_grant` marks the connection for reauthorization. After a 401 the
connector asks once for a refresh, which project storage performs only when the
recorded expiry has passed, and then retries the request once; otherwise the
401 selects `authorizationRevoked`.

The operation has these branches:

| Branch | Meaning |
| --- | --- |
| `listed` | One bounded page was loaded. |
| `authorizationRevoked` | The access token is invalid or revoked. |
| `accessDenied` | Spotify does not allow this user to read the playlist items. |
| `notFound` | Spotify did not find the playlist. |
| `providerRejected` | Spotify conclusively rejected the query. |
| `invalidResponse` | Spotify returned malformed, unsafe, or oversized data. |
| `defect` | Input, connection configuration, or connector definition is invalid. |

HTTP 429 honors Spotify's `Retry-After` seconds, with a one-minute fallback.
HTTP 5xx and transport failures use the generated bounded Dex retry policy.
Other provider outcomes are terminal branches and are never copied from raw
response bodies.

Playlist pagination is not a snapshot: edits between calls can move items.
Applications that need another page should persist their business context and
invoke another Connector Step with the returned `nextOffset`.

## Mocks

`spotifymock.New` supplies a typed connection. Its default describes playlist
`3cEYpjA9oz9GiPac4AsH4n` with five items across three offset pages of size
two. Each page has one available track; the first two also skip an unavailable
item. Script other playlist IDs, offsets, or page sizes explicitly. Named cases model a
revoked grant, inaccessible or missing playlist, and rejected query.

The [runnable mock integration test](go/examples/playlist-tracks/flow/workflow_mock_integration_test.go)
uses the connection on a real Dex Worker:

```go
mock := spotifymock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
client := startPlaylistWorker(t, flow)
input := Input{PlaylistID: "3cEYpjA9oz9GiPac4AsH4n", Limit: 2}
result := runPlaylistFlow(t, client, flow, fmt.Sprintf("spotify-playlist-mock-%d", time.Now().UnixNano()), input)
require.Equal(t, dex.FlowCompleted, result.Status, "Flow failed: %s", result.ErrorMessage)
```

The result deliberately excludes preview audio and artwork. Track, artist, and
album metadata includes Spotify URLs for attribution. Applications displaying
the data must follow Spotify's
[Design and Branding Guidelines](https://developer.spotify.com/documentation/design)
and [Developer Policy](https://developer.spotify.com/policy), including links
back to Spotify and the restrictions on downloads and AI training.

The [playlist-tracks example](go/examples/playlist-tracks/README.md) is a runnable
FDG 2.0 Start Flow that lists one page and displays it in Dex Web. Its
[`main.go`](go/examples/playlist-tracks/main.go) loads the project configuration
that Dex Web or Superverse Studio writes and opens the connection by name:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := spotify.NewProjectConnection(project, playlisttracks.ConnectionName)
if err != nil {
	return err
}
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md).

## Test

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

Run the real Dex example integration against the repository's development
server:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/playlist-tracks/... -count=1 -v
```

An opt-in live test requires a dedicated OAuth token with exactly
`playlist-read-private` and an owned or collaborative playlist:

```bash
GOWORK=off \
  SPOTIFY_CONNECTOR_TEST_TOKEN=... \
  SPOTIFY_CONNECTOR_TEST_PLAYLIST_ID=... \
  go test -tags=live ./... -count=1 -v
```
