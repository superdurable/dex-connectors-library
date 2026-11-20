# Useasy TypeScript example

This runnable Flow calls `listAgents` with static connection name `useasy` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '{"page":2,"pageSize":10}'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.
