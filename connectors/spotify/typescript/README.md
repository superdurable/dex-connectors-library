# Spotify TypeScript connector

`@superdurable/connector-spotify` lists one bounded page of tracks from a Spotify playlist in a Dex Flow. The package implements `listPlaylistTracks` and uses the `spotify-playlist-reader` connection in its runnable example.

Configure the connection in Dex Web with a Spotify developer application from [Spotify for Developers](https://developer.spotify.com/dashboard). In the app's Settings, register Dex Web's displayed redirect URI and enable Web API access. Authorize the `playlist-read-private` scope. Dex stores and refreshes the OAuth access token; the client secret is optional for PKCE applications. Spotify's public Web API base URL, 15 second request timeout, and 4 MiB response limit are defaults. Change them only when the connection requires another compatible endpoint or a tighter request bound.

To run the checked-in example, start `dexcli dev` and configure the named connection in Dex Web. Then run `npm run example:list` to start its Worker and `npm run example:list:start -- examples/playlist-tracks/input.example.json` in another terminal. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex runs on a nondefault address. The example's [Flow](examples/playlist-tracks/flow/workflow.ts) stores only bounded track metadata; its token stays in the connection.

Run `npm run check` for package tests and static checks. Run `DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run test:integration` with a running Dex dev server for the Worker and Client test. The integration test binds its Spotify stand-in to an OS assigned port.
