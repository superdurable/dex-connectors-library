# YouTube connector for Dex Python

This package implements `searchVideos`, `getVideo`, `getChannel`, `listPlaylistItems`, and `addPlaylistItem` against the YouTube Data API v3. It uses the manifest-generated Google OAuth refresh wiring. The read operations accept a read-only or manage connection; `addPlaylistItem` requires `google-oauth-manage` and checks for an existing playlist item before inserting.

The runnable Flow is in `examples/playlist_curator/`. Register the connection in Dex Web under the static name `youtube-playlist-curator` and follow the OAuth guide there. Run the example with `DEX_FLOW_SERVICE_ADDRESS` set to a running `dexcli dev` server. The checked-in conformance suite replays Go-recorded provider responses; live Google authorization and API behavior have not been verified.
