# ZoomEye TypeScript example

This Flow calls `getAccountQuota` with static connection name `zoomeye` and persists subscription balances in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start` in another terminal.

Use `dexcli dev -open=false` for local testing. Credentials stay in Dex Connections, outside Flow input and state. Optional rejection branches are unwired and fail the Flow.
