# List Hiver shared inboxes

`ListHiverInboxesFlow` lists one page of shared inboxes through the static `hiver-support` connection. The request and returned page are registered as durable Attributes. The `listed` branch completes the Flow; optional provider rejection, invalid response, and defect branches are unwired and fail it.

From `connectors/hiver/typescript`, run `npm ci` and `npm run build`. Configure the `hiver-support` connection in Dex Web using the [Hiver setup guidance](../../../README.md), start `dexcli dev`, then run `npm run example:list`. In a second terminal, run `npm run example:list:start -- examples/list-inboxes/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` if the dev server is not at the default address.

The integration test runs this same Flow with a local Hiver-compatible provider and a real Dex Worker and Client. Start the dev server, set `DEX_FLOW_SERVICE_ADDRESS`, and run `npm run test:integration`.

For strict FDG 2.0 generation from this directory, use `dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/hiver-list-inboxes`. The source declares a group and explanation for each hand-written Step; the generated connector Step has its own annotation.

Dex CLI 1.33.0 currently reports `dynamic_step_target` and `unknown_step_target` for the generated connector Step factory, plus a `dynamic_class` diagnostic for its target. The real Worker and Client integration test passes; strict FDG 2.0 lint for this factory awaits a Dex CLI analyzer fix.
