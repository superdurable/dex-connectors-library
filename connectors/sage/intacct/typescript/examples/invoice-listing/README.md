# Sage Intacct invoice listing (TypeScript)

This runnable Flow calls `newListInvoicesStep` with the static `sage-intacct` Dex Web connection and persists the invoice keys, IDs, and opaque next link. Configure OAuth as described in the [connector guide](../../../README.md), run `npx tsx examples/invoice-listing/worker.ts`, then run `npx tsx examples/invoice-listing/start.ts` in another terminal.

From the TypeScript package directory, link this unreleased local package, then generate and lint its FDG 2.0 graph:

```sh
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-sage-intacct
dexcli visualize examples/invoice-listing/flow.ts --schema-version 2.0 --lint app --open=false --json --out /tmp/sage-intacct-typescript-fdg
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture. Live Sage Intacct authorization requires a user-controlled account.
