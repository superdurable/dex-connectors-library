# Caiyun current weather example

`flow.ts` runs `getCurrentWeather` with the static `caiyun` Dex Web connection and persists only metric weather values. The Flow input is longitude and latitude in degrees; mainland China uses GCJ-02 coordinates. Configure the App Key and App Secret through the connector's Dex Web guide.

Run `dexcli dev -open=false`, then `npm run example:worker` in one terminal and `npm run example:start -- 116.3176 39.976` in another. For local FDG analysis before this package is published, run `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-caiyun` once from the TypeScript package directory. Generate FDG 2.0 with `dexcli visualize --open=false --schema-version 2.0 examples/current-weather/flow.ts`.

The integration test uses a local provider fake. Real Caiyun authorization and observations remain unverified.
