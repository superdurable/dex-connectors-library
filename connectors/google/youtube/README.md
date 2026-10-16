# YouTube Connector

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
[`examples/playlist-curator/main.go`](examples/playlist-curator/main.go) does
for one connection of each method. Tokens refresh before expiry and once after
an HTTP 401; terminal `invalid_grant` responses require reauthorization. An
OAuth app in **Testing** publishing status issues refresh tokens that Google
revokes after 7 days.

## Idempotency

`playlistItems.insert` accepts no idempotency key. `AddPlaylistItem` therefore
lists the playlist filtered to the video before every insert and returns
`added` with `wasAlreadyPresent: true` when the playlist already holds it. An
ambiguous insert, such as a timeout or HTTP 5xx after dispatch, retries, and
the retried attempt's check finds an item the failed attempt stored. The
operation uses sync durability so attempts never overlap. Residual risk: a
playlist read that lags a just-completed insert could let a duplicate through,
and the operation cannot intentionally add a video a playlist already holds.

## Branches

Every operation uses `defect` for invalid local input or missing credentials,
`quotaExhausted` when the project's daily quota (10,000 units by default) is
spent, `providerRejected` for conclusive refusals such as HTTP 403 or 400, and
`invalidResponse` for malformed or oversized responses. Reads of an unknown
video, channel, or playlist use `notFound`. Rate limits, timeouts, and HTTP 5xx
retry instead of producing a branch. Failure messages carry Google's documented
error reason, never its message text. Titles and descriptions are untrusted
provider text.

## Example

[`examples/playlist-curator`](examples/playlist-curator) is a runnable Dex Web
**Start Flow** example that adds a channel's newest matching video to a
playlist and reads the playlist back to confirm.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```
