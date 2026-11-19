# Tuya TypeScript example

This Flow calls `getDeviceDetails` with static connection name `tuya` and stores the selected result in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- '{"deviceId":"device123"}'` in another terminal.

Use `dexcli dev -open=false` for local testing. Credentials stay in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow.
