# Sage Intacct invoice inspection (TypeScript)

This runnable Flow calls `newGetInvoiceStep` with the static `sage-intacct` Dex Web connection and persists the returned invoice key and ID. Configure OAuth as described in the [connector guide](../../../README.md), run `npm run example:worker`, then set `SAGE_INTACCT_INVOICE_KEY` to an accessible invoice's system key and run `npm run example:start`.

From the TypeScript package directory, link this unreleased local package, then generate and lint its FDG 2.0 graph:

```sh
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-sage-intacct
dexcli visualize examples/invoice-inspection/flow.ts --schema-version 2.0 --lint app --json --out /tmp/sage-intacct-typescript-fdg
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture. Live Sage Intacct authorization requires a user-controlled account.
