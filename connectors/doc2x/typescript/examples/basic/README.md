# Doc2X TypeScript example

This Flow calls `getParseStatus` for a PDF task with static connection name `doc2x` and persists the selected result in a Dex Attribute. Configure the connection in Dex Web, start `dexcli dev -open=false`, run `npm run example:worker`, then run `npm run example:start -- '{"uid":"TASK_UID"}'` in another terminal.

The API Key stays in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow. The local integration test runs the same Flow through a real Dex Worker and Client with a safe fake provider.
