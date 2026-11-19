# UPYUN TypeScript example

This Flow calls `getServiceUsage` with static connection name `upyun` and stores its selected result in a Dex Attribute. Configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- '{}'` in another terminal.

Use `dexcli dev -open=false` for local testing. The provider credentials stay in the Dex connection store, outside Flow input and state.
