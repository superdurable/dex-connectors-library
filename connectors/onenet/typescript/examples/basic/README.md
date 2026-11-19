# Onenet TypeScript example

This runnable Flow calls `queryDeviceDetail` with static connection name `onenet` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '<JSON input>'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.

For FDG 2.0 verification from the repository root after `npm ci --prefix connectors/onenet/typescript`, register the local package manifest and run:

```sh
mkdir -p connectors/onenet/typescript/node_modules/@superdurable
ln -sfn ../.. connectors/onenet/typescript/node_modules/@superdurable/connector-onenet
dexcli visualize connectors/onenet/typescript/examples/basic/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/onenet-ts-fdg --open=false
```
