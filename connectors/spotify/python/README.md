# Spotify Python connector

Install `dex-connector-spotify==0.1.0`. The `listPlaylistTracks` Query lists one bounded page of an owned or collaborative Spotify playlist. It accepts a 22-character playlist ID, optional two-letter market, limit from 1 to 50 (zero selects 20), and zero-based offset. The result skips unavailable items and returns `nextOffset` for the next request.

`new_project_connection` loads the static connection name and configuration from Dex project configuration. Generated OAuth wiring resolves private credentials at call time, refreshes before expiry, and coordinates one refresh after a rejected token. The connector sends no credentials through Flow input or results. Use [the runnable example](examples/playlist_tracks/README.md) with `dexcli dev`.

Spotify 429 and 5xx responses and transport errors retry. Revoked authorization, denied access, missing playlists, provider rejection, invalid responses, and invalid local input select the matching terminal branch. A connection can be created with `Client`, a typed credential provider, and `new_connection` for custom applications. Close the client after the Worker stops. It owns and closes an injected HTTP transport.

Run `uv run --with-editable ../../../sdkpython pytest` from this directory, and run the Go conformance recording in `../go` when fixtures change. Live Spotify OAuth and playlist access require an application configured in the [Spotify developer dashboard](https://developer.spotify.com/dashboard); they have not been verified by the checked-in tests.
