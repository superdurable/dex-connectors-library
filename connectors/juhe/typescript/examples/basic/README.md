# Juhe TypeScript example

This Flow calls `getCurrentWeather` with static connection name `juhe` and stores the selected current conditions in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- '{"city":"北京"}'` in another terminal.

Use `dexcli dev -open=false` for local testing. Credentials stay in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow.
