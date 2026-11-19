# EMQX Cloud TypeScript example

This Flow calls `listDeployments` with static connection name `emq` and persists the selected result in a Dex Attribute. Configure the connection in Dex Web, start `dexcli dev -open=false`, run `npm run example:worker`, then run `npm run example:start -- '{}'` in another terminal.

The Platform API credentials stay in the Dex connection store, outside Flow input and state. Optional rejection branches are unwired and fail the Flow. The local integration test runs the same Flow through a real Dex Worker and Client with a fake provider.
