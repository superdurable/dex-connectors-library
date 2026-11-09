# Microsoft Advertising campaign discovery (TypeScript)

This runnable Flow calls `newListCampaignsStep` using the static `microsoft-advertising` Dex Web connection and persists the returned campaign list. Configure OAuth and the developer token as described in the [connector guide](../../../README.md), run `npm run example:worker`, then set `MICROSOFT_ADVERTISING_ACCOUNT_ID` to the numeric `aid` from the Campaigns URL and run `npm run example:start`.

From the TypeScript package directory, link this unreleased package and generate its FDG 2.0 graph:

```sh
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-microsoft-advertising
dexcli visualize examples/campaign-discovery/flow.ts --schema-version 2.0 --lint app --json --out /tmp/microsoft-advertising-typescript-fdg
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, `npm run test:integration` exercises the Flow through a real Dex Worker and Client against a local provider fixture.
