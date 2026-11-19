# Summarize text with the LLM connector

`SummarizeTextFlow` sends a provider-neutral text request through the static
`llm` connection and persists streamed text. The connected provider and model
are selected in Dex Web; the application input contains no credential.

Start Dex locally, configure the `llm` connection through Dex Web, and set
`DEX_CONNECTOR_CONFIG_FILE` to its local mode-0600 `connections.json` file.
From `connectors/superdurable/llm/typescript`, build the package and start
the Worker:

```sh
npm ci
npm run build
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:34110 npx tsx examples/summarize-text/main.ts
```

In another terminal with the same project configuration:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:34110 npx tsx examples/summarize-text/start.ts examples/summarize-text/input.example.json
```

The starter prints the generated text and usage. The input example is safe to
edit and contains no key. The real Dex integration test uses the same Flow and
a local provider stand-in:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:34110 npm run test:integration
```

Generate and lint the Flow Definition Graph 2.0 from
`examples/summarize-text/flow.ts` with `dexcli visualize` after installing
the local package into that example's analyzer directory. The integration
test verifies retry, stream persistence, and an unwired optional branch.
