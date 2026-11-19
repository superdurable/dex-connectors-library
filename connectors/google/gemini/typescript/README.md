# Gemini TypeScript connector

`@superdurable/connector-google-gemini` provides the native `generateContent` Query for Gemini's `models.generateContent` API. It supports Gemini contents, structured JSON output, thinking budgets, token usage, and explicit safety outcomes.

## Run the example

The [generate-summary example](examples/generate-summary/flow/workflow.ts) declares a static `gemini-summary` connection and wires the `generated` branch. Configure a `gemini-api-key` connection in Dex Web using a Google AI Studio API key, then run `dexcli dev`. Set `DEX_FLOW_SERVICE_ADDRESS` to the Dex gRPC address and start the Worker with `npm run example:summary`. In another terminal, run `npm run example:summary:start -- "Summarize this launch"`.

The Worker loads the connection from the Dex project environment. The key is resolved for each provider call and sent only in the `x-goog-api-key` header. The example persists the safe generated result, including token usage. Optional blocked, rejected, truncated, and invalid-response branches are unwired and fail the Flow if selected.

The TypeScript package implements `generateContent` only; it has no Trigger. The Go connector's [README](../README.md) explains request fields, branch behavior, connection defaults, and the model picker. Local conformance replay and the example's Dex integration test use a provider stand-in. Real Gemini API authorization and generation remain unverified for this package.
