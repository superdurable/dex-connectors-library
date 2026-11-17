# YouTube TypeScript connector

`@superdurable/connector-google-youtube` provides Dex Query Steps for video search, video and channel reads, and playlist pages, plus a playlist Mutation that adds a video unless the playlist holds it. After an unknown insert outcome it reads the playlist back and resends while possible inserts stay below the send cap (`maxSends`, default 3), and selects `uncertain` only at the cap when the listing does not show the video. The package follows the Go connector's YouTube Data API v3 request and retry behavior. OAuth credentials are resolved and refreshed through the host-owned connection using the generated authorization declaration.

The checked-in [video lookup example](examples/video-lookup/README.md) binds the static `youtube-video-reader` connection and runs a real Dex Flow. Configure Google OAuth in Dex Web Connections. The read-only method is sufficient for the example; `addPlaylistItem` requires the manage method.

## Verify

From this package directory, run `npm ci`, `npm run check`, and `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` set to a running `dexcli dev` server. The conformance test replays Go-recorded provider requests, branches, retries, and outputs. The integration test uses a local YouTube stand-in through a real Dex Worker and Client.

The TypeScript package is `v0.1.0`. A live Google OAuth grant and live YouTube API call have not been verified by this example's local integration test.
