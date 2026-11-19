# Easemob TypeScript example

This Flow calls `getUserDetail` with static connection name `easemob` and persists the selected account fields in a Dex Attribute. Configure the connection in Dex Web, start `dexcli dev -open=false`, run `npm run example:worker`, then run `npm run example:start -- '{"userId":"user1"}'` in another terminal.

The Client Secret and App Token stay outside Flow input and state. Optional rejection branches are unwired and fail the Flow. The integration test uses a fake token and user endpoint with a real Dex Worker and Client.
