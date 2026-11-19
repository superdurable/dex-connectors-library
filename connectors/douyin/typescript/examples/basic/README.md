# Douyin TypeScript example

This Flow calls `getBehaviorSummary` with static connection name `douyin` and persists aggregate results in a Dex Attribute. Configure the connection in Dex Web, start `dexcli dev -open=false`, run `npm run example:worker`, then run `npm run example:start -- '{"date":"YYYY-MM-DD"}'` for an available reporting day in another terminal.

The client secret and short-lived token stay outside Flow input and state. Optional rejection branches are unwired and fail the Flow. The integration test uses a fake token and analytics endpoint with a real Dex Worker and Client.
