# YouTube Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/google/youtube/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/google/youtube`.

The YouTube Connector calls the YouTube Data API v3 and exposes
operation-specific Dex Step factories:

- `youtube.NewSearchVideosStep` searches public videos by keyword or channel
  (100 quota units per call);
- `youtube.NewGetVideoStep` reads one video's details and statistics;
- `youtube.NewGetChannelStep` reads one channel by ID, handle, or the
  authorized account's own channel, including its uploads playlist;
- `youtube.NewListPlaylistItemsStep` pages through a playlist, optionally
  filtered to one video;
- `youtube.NewAddPlaylistItemStep` adds a video to a playlist the account owns,
  at most once.

## Scopes

Manifest scopes are per authorization method, not per operation, so the
connector offers two OAuth methods and a Flow binds each Step to the connection
whose scope it needs:

| Method | Connection kind | Scope | Operations |
| --- | --- | --- | --- |
| `google-oauth-readonly` (default, recommended) | `google-youtube-readonly-oauth` | `youtube.readonly` | `searchVideos`, `getVideo`, `getChannel`, `listPlaylistItems` |
| `google-oauth-manage` | `google-youtube-oauth` | `youtube` | `addPlaylistItem`, and every read |

`addPlaylistItem` on a read-only connection selects `providerRejected` with an
`AUTHORIZATION` failure before any YouTube call. A refreshed token must carry
its method's scope; `youtube` also satisfies the read-only method. The constants
`youtube.ReadOnlyAuthMethodID` and `youtube.ManageAuthMethodID` name the
methods.

The generated `Credentials` keeps the OAuth client, access token, and refresh
token in the project connection that `NewProjectConnection` opens, as
[`examples/playlist-curator/main.go`](go/examples/playlist-curator/main.go) does
for one connection of each method. Tokens refresh before expiry and once after
an HTTP 401; terminal `invalid_grant` responses require reauthorization. An
OAuth app in **Testing** publishing status issues refresh tokens that Google
revokes after 7 days.

## Idempotency

`playlistItems.insert` accepts no idempotency key, and YouTube does not promise
that `playlistItems.list` shows an item that was just inserted. The first
attempt of `AddPlaylistItem` lists the playlist filtered to the video and
returns `added` with `wasAlreadyPresent: true` when the playlist already holds
it. Otherwise it sends one insert through a single-dispatch fence
(`execution.dispatchFence`, which requires sync durability). Only an insert
that YouTube provably did not process, after HTTP 429, a `rateLimitExceeded`
or `userRateLimitExceeded` refusal, or a connection that never opened, may be
sent again on a later attempt.

After any other failure that may have applied the insert, such as a timeout,
HTTP 500 or 503 (Google documents `backendError` without saying the write did
not happen), or a malformed or oversized 2xx response, the next attempt only
lists the playlist. It returns `added` when the video is shown and `uncertain`
when it is not, because the listing can lag behind the insert. Wire
`uncertain` to a Step that checks the playlist before adding the video again.
An interrupted attempt that leaves no checkpoint also leads to this read-back.
The operation cannot intentionally add a video a playlist already holds.

## Agent tools

All five operations carry a reviewed `agent` block, so an agent host may offer
them as tools. The four reads change nothing, so each optional branch is
`rejected`. `addPlaylistItem` recommends approval because it changes a playlist
that other people may see. It does not claim `retry: safe`, and its
`invalidResponse` and `uncertain` branches are `unknown`, so a host reports
them as unconfirmed instead of letting a model repeat the call.

## Branches

Every operation uses `defect` for invalid local input or missing credentials,
`quotaExhausted` when the project's daily quota (10,000 units by default) is
spent, `providerRejected` for conclusive refusals such as HTTP 403 or 400, and
`invalidResponse` for malformed or oversized responses. Reads of an unknown
video, channel, or playlist use `notFound`. Rate limits, timeouts, and HTTP 5xx
retry instead of producing a branch; for `addPlaylistItem`, see
[Idempotency](#idempotency) for when a retry only reads the playlist back and
selects `uncertain`. Failure messages carry Google's documented
error reason, never its message text. Titles and descriptions are untrusted
provider text.

## Mocks

The generated `youtubemock` package models one Compiler Lab channel, its
walkthrough video, and a curated playlist. Search and playlist listings have
three-page cursor chains with a repeated item. The
[real-Dex example test](go/examples/playlist-curator/flow/workflow_mock_integration_test.go)
uses separate read and manage connections:

```go
readMock := youtubemock.New(t, ReadConnectionName)
manageMock := youtubemock.New(t, ManageConnectionName)
flow := NewFlow(readMock.Connection(), manageMock.Connection())
```

Each operation has an authorization rejection and quota exhaustion case.
Reads and the playlist write also offer `notFound` where the provider can
report an absent resource. Script a case for other channel, video, or playlist
inputs.

## Example

[`examples/playlist-curator`](go/examples/playlist-curator) is a runnable Dex Web
**Start Flow** example that adds a channel's newest matching video to a
playlist and reads the playlist back to confirm.

## Python package

[`python/`](python/) releases `dex-connector-google-youtube` v0.1.0 independently
of Go. It implements all five operations with the generated OAuth connection,
refresh driver, and operation-specific Step factories. The
[`video_search` example](python/examples/video_search/) starts a Flow through
the static `youtube-playlist-curator` connection. The Python conformance suite
replays Go-recorded provider requests and outcomes, including retry and
check-before-insert paths. Live Google OAuth and YouTube API behavior remain
unverified.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

## TypeScript package

The `typescript/` package declares `@superdurable/connector-google-youtube` v0.1.0 with all five operations and no Triggers. It uses generated Dex Step factories and OAuth refresh wiring. The runnable [video lookup Flow](typescript/examples/video-lookup/README.md) shows a read-only Google OAuth connection named `youtube-video-reader`; adding a playlist item requires the manage authorization method. The package's Go-recorded conformance fixtures and real Dex Worker integration test run locally with a provider stand-in. Live Google OAuth and YouTube API behavior remain unverified.
