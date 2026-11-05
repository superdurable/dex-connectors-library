# List staged Webflow items

The `WebflowListItems` Flow calls `listCollectionItems` using the static
`webflow` connection and completes with the provider's page. Start the Worker
with `npm run example:list-items` after configuring `site_token` in Dex Web.
Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a nondefault port.

With `dexcli dev` running, `npm run integration:list-items` exercises this
same Flow with a scripted provider response and a retried rate limit.
Use `dexcli visualize examples/list-items/flow.ts --language typescript
--schema-version 2.0 --lint app --json --out /tmp/webflow-list-items` from the
TypeScript package directory for strict FDG 2.0 generation.

The current Dex CLI static analyzer reports `dynamic_class` for the
connector Step target in the factory configuration and
`dynamic_step_target` for the registered QueryStep. The Flow passed the real
Worker and Client integration test; strict FDG output remains unverified until
the CLI accepts this generated Step factory pattern.
