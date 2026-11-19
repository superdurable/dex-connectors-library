# Netease TypeScript example

This runnable Flow calls `getUserProfile` with static connection name `netease` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '<JSON input>'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.

For FDG 2.0 verification from the repository root after `npm ci --prefix connectors/netease/typescript`, register the local package manifest and run:

```sh
mkdir -p connectors/netease/typescript/node_modules/@superdurable
ln -sfn ../.. connectors/netease/typescript/node_modules/@superdurable/connector-netease
dexcli visualize connectors/netease/typescript/examples/basic/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/netease-ts-fdg --open=false
```
