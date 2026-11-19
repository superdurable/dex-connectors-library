# Zadig TypeScript example

This Flow calls `listProjects` with static connection name `zadig` and stores its result in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- '{ pageNum: 2, pageSize: 10 }'` in another terminal.

Use `dexcli dev -open=false` for local testing. Credentials stay in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow.
