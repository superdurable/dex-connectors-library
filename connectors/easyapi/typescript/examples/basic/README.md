# EasyAPI TypeScript example

This Flow calls `queryShop` with static connection name `easyapi` and persists the selected result in a Dex Attribute. Configure the connection in Dex Web, start `dexcli dev -open=false`, run `npm run example:worker`, then run `npm run example:start -- '{"taxNumber":"91320211MA1WML8X6T"}'` in another terminal.

Credentials stay in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow. The integration test runs the same Flow through a real Dex Worker and Client with a fake provider.
