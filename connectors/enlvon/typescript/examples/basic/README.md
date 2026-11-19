# Enlvon TypeScript example

This Flow calls `searchPolicies` with static connection name `enlvon` and persists the selected result in a Dex Attribute. Configure the connection in Dex Web, start `dexcli dev -open=false`, run `npm run example:worker`, then run `npm run example:start -- '{"query":"分时电价","topK":1}'` in another terminal.

The API Key stays in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow. The integration test runs the same Flow through a real Dex Worker and Client with a fake provider.
