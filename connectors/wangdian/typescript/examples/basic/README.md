# Wangdian TypeScript example

This runnable Flow calls `listShops` with static connection name `wangdian` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '{"page":2,"pageSize":10}'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.
