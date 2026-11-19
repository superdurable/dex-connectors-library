# Meituan TypeScript example

This runnable Flow calls `queryOrderStatus` with static connection name `meituan` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '<JSON input>'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.

For FDG 2.0 verification from the repository root after `npm ci --prefix connectors/meituan/typescript`, register the local package manifest and run:

```sh
mkdir -p connectors/meituan/typescript/node_modules/@superdurable
ln -sfn ../.. connectors/meituan/typescript/node_modules/@superdurable/connector-meituan
dexcli visualize connectors/meituan/typescript/examples/basic/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/meituan-ts-fdg --open=false
```
