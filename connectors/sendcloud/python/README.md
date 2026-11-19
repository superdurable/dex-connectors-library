# sendcloud Python connector

`dex-connector-sendcloud` implements the manifest-declared `getAccountBalance` Query for Dex Flows. See `../README.md` for provider setup and credentials.

The checked-in example under `examples/` runs a real Dex Worker and Client. `uv run pytest` replays the Go-recorded provider scenarios. Live provider authorization has not been verified.
