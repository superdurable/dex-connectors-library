# Playlist tracks Flow

This example uses the static `spotify-playlist-reader` Dex Web connection and lists one bounded playlist page. Its Worker starts in `main.ts`; `start.ts` submits `input.example.json` to Dex and prints the page. Complete Spotify OAuth setup in Dex Web as described in the [TypeScript package README](../../README.md).

From `connectors/spotify/typescript`, run `npm ci`, then `npm run example:list`. In another terminal run `npm run example:list:start -- examples/playlist-tracks/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex endpoint. `npm run test:integration` exercises this Flow through a real Dex Worker and Client against a local Spotify stand-in.
