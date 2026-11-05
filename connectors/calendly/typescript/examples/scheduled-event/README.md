# Scheduled event Flow

This example uses the static `calendly-scheduling` Dex Web connection and reads one scheduled event. Its Worker starts in `main.ts`; `start.ts` submits `input.example.json` to Dex and prints the event. Configure a Calendly personal access token or OAuth connection in Dex Web as described in the [TypeScript package README](../../README.md).

From `connectors/calendly/typescript`, run `npm ci`, then `npm run example:event`. In another terminal run `npm run example:event:start -- examples/scheduled-event/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` for a nondefault Dex endpoint. `npm run test:integration` exercises this Flow through a real Dex Worker and Client against a local Calendly stand-in.
