# Polyv TypeScript example

This runnable Flow calls `listChannels` with static connection name `polyv` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '<JSON input>'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.

For FDG 2.0 verification from the repository root after `npm ci --prefix connectors/polyv/typescript`, register the local package manifest and run:

```sh
mkdir -p connectors/polyv/typescript/node_modules/@superdurable
ln -sfn ../.. connectors/polyv/typescript/node_modules/@superdurable/connector-polyv
dexcli visualize connectors/polyv/typescript/examples/basic/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/polyv-ts-fdg --open=false
```
