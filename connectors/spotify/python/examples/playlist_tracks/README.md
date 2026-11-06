# Spotify playlist tracks Python example

Create a Dex Web connection named `spotify-playlist-reader`, using a Spotify application configured for OAuth and the `playlist-read-private` scope. Set `DEX_FLOW_SERVICE_ADDRESS` to the address of a running `dexcli dev` server and run `python -m examples.playlist_tracks.main` from `connectors/spotify/python`. Start `SpotifyPlaylistTracksPython` with a JSON input such as `{"playlistId":"3cEYpjA9oz9GiPac4AsH4n","limit":2}`. The Flow stores public playlist metadata and completes with one page. Optional failure branches are intentionally unwired and fail the Flow when selected.

The integration test starts a real Dex Worker and Client against `dexcli dev` while a local HTTP stand-in supplies Spotify responses. It does not verify the live Spotify OAuth consent or playlist endpoint.
