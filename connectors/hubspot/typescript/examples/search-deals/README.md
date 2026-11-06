# Search deals Flow

This example uses the static `hubspot-crm` Dex Web connection to search one bounded page of deals. The Worker entrypoint is `main.ts`; `start.ts` submits `input.example.json` and prints the page. Configure a HubSpot private app token or OAuth connection as described in the [TypeScript package README](../../README.md).

From `connectors/hubspot/typescript`, run `npm ci`, then `npm run example:search`. In another terminal run `npm run example:search:start -- examples/search-deals/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex endpoint. `npm run test:integration` exercises this Flow through a real Dex Worker and Client against a local HubSpot stand-in.
