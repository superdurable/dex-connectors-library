# Oracle NetSuite customer inspection (TypeScript)

This runnable Flow calls `newGetCustomerStep` with the static `oracle-netsuite` Dex Web connection and persists the returned customer internal and entity IDs. Configure OAuth as described in the [connector guide](../../../README.md), run `npm run example:worker`, then set `ORACLE_NETSUITE_CUSTOMER_ID` to an accessible customer internal ID and run `npm run example:start`.

From the TypeScript package directory, link this unreleased local package, then generate and lint its FDG 2.0 graph:

```sh
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-oracle-netsuite
dexcli visualize examples/customer-inspection/flow.ts --schema-version 2.0 --lint app --json --out /tmp/oracle-netsuite-typescript-fdg
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture. Live Oracle NetSuite authorization requires a user-controlled account.
