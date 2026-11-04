# Generate a Gemini summary

This runnable Flow binds the static `gemini-summary` Connection, maps start text into one `generateContent` request, and completes with Gemini's generated result. Optional outcome branches fail the Flow unless an application wires a business reaction.

1. Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address.
2. Configure `gemini-summary` as a `gemini-api-key` Connection in Dex Web using a Google AI Studio key. The manifest's authorization guide links to the key page.
3. Run `npm run example:summary` from the TypeScript package directory with the Dex project environment.
4. Run `npm run example:summary:start -- "Summarize this launch"` from another terminal.

The integration test uses a local provider stand-in and a real Dex Worker and Client. It requires `DEX_FLOW_SERVICE_ADDRESS` and runs with `npm run test:integration`.

Generate and lint its strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer. From `connectors/google/gemini/typescript`, build the package, install it into the example for static analysis, then run:

```sh
npm run build
npm install --prefix examples/generate-summary --no-save --no-package-lock --ignore-scripts .
cd examples/generate-summary
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
