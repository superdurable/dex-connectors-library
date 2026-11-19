# Qichacha TypeScript example

This runnable Flow calls `getEnterpriseBasicDetails` with static connection name `qichacha` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '{"keyword":"示例科技有限公司"}'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.
