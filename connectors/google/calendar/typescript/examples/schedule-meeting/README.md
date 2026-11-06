# Meeting lookup Flow

This runnable Flow reads one Google Calendar event with the static `google-calendar` connection. Configure that connection in Dex Web using the [authorization guide](../../../README.md#authorization), then run `npm ci` and `npm run example:calendar`. In another terminal, run `npm run example:calendar:start -- examples/schedule-meeting/input.example.json`. Set `DEX_FLOW_SERVICE_ADDRESS` when Dex uses a nondefault gRPC port.

The Flow persists only returned event metadata. An unwired `providerRejected` branch fails the Flow. `npm run test:integration` verifies both outcomes through a real Dex Worker and Client against a local Google stand-in.

Generate and lint its strict Flow Definition Graph 2.0 from `connectors/google/calendar/typescript`:

```sh
npm run build
npm install --prefix examples/schedule-meeting --no-save --no-package-lock --ignore-scripts .
cd examples/schedule-meeting
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
