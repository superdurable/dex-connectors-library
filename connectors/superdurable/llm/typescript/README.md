# Super Durable LLM TypeScript connector

`@superdurable/connector-superdurable-llm` runs the manifest's `generateText`
Query through the provider selected by the static Dex Web connection. The
package supports OpenAI Responses, Claude Messages, Gemini generateContent, and
the seven Chat Completions providers listed in the
[connector README](../README.md). Model IDs belong to the selected provider.

The runnable [summarize-text example](examples/summarize-text/README.md)
contains the application code for `newProjectConnection`,
`newGenerateTextStep`, a Worker, and a starter. The public Step wires only
`generated`; each other optional branch fails the Flow when selected.

Configure the `llm` connection in Dex Web using the
[connection guide](../README.md#connection). The key stays in project
credential storage and is resolved for each call. A changed provider, model,
region, or endpoint requires restarting the Worker; replacing the key does not.
The TypeScript package requires `@superdurable/connectors-sdk` 0.3.0 and Dex
TypeScript SDK 1.28.0 or newer. OpenAI-compatible endpoints use the built-in
HTTP transport: the SDK checks every DNS answer, pins the dialed address, and
keeps the original HTTPS host for TLS verification. Fixed providers use the
same public HTTPS policy. A custom
`httpClient` is unsupported for all providers.

From this package directory:

```sh
npm ci
npm run check
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:34110 npm run test:integration
```

The integration test needs `dexcli dev -dex-port 34110 -web-port 34111
-open=false` and uses a local provider stand-in. The conformance test replays
Go-recorded provider responses. Live provider authorization and generation
remain unverified.
