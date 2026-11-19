# Jushuitan TypeScript example

This runnable Flow calls `listShops` with static connection name `jushuitan` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '<JSON input>'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.

For FDG 2.0 verification from the repository root after `npm ci --prefix connectors/jushuitan/typescript`, register the local package manifest and run:

```sh
mkdir -p connectors/jushuitan/typescript/node_modules/@superdurable
ln -sfn ../.. connectors/jushuitan/typescript/node_modules/@superdurable/connector-jushuitan
dexcli visualize connectors/jushuitan/typescript/examples/basic/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/jushuitan-ts-fdg --open=false
```
