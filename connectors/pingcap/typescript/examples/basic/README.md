# Pingcap TypeScript example

This runnable Flow calls `listProjects` with static connection name `pingcap` and persists the provider result in a Dex Attribute. Configure the connection through Dex Web, start `dexcli dev -open=false`, then run `npm run example:worker` and `npm run example:start -- '<JSON input>'` in separate terminals. Credentials remain outside Flow input and state. Optional provider rejection branches are unwired.

For FDG 2.0 verification from the repository root after `npm ci --prefix connectors/pingcap/typescript`, register the local package manifest and run:

```sh
mkdir -p connectors/pingcap/typescript/node_modules/@superdurable
ln -sfn ../.. connectors/pingcap/typescript/node_modules/@superdurable/connector-pingcap
dexcli visualize connectors/pingcap/typescript/examples/basic/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/pingcap-ts-fdg --open=false
```
