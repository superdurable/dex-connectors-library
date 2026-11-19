# Geovisearth TypeScript example

This Flow calls `getCurrentWeather` with static connection name `geovisearth` and stores the selected result in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- '{"location":"WTX_CH101300201"}'` in another terminal.

Use `dexcli dev -open=false` for local testing. Credentials stay in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow.
