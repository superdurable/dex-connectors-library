# Confluence page-changed Flow for TypeScript

This example starts one `ConfluencePageChangeTypeScript` Flow for every page
created or updated in the picked space, through the `pageChanged` Trigger, at
parity with the [Go example](../../../go/examples/page-changed/README.md):

1. `RecordPageChange` validates the delivered change and records it.
2. `ReadChangedPage` calls `newGetPageStep` with the page ID and reads the page
   back as Markdown, at most 2,000 characters, so the record holds its current
   title and body.
3. `CompletePageChange` records the page read back and completes the Flow.

The Flow ID is `confluence-page-change-<Trigger event ID>`, and the event ID is
stable per page version, so a redelivered change finds its existing Flow. Only
the happy-path `found` branch is wired; a missing, rejected, or invalid read
fails the Flow. The first start records recently changed pages as a baseline
and starts no Flow for them.

## Configure

Follow the [Confluence connector setup](../../../README.md#atlassian-setup),
then configure the `confluence-page-watcher` connection in Dex Web
**Connections**. **Poll interval** is the delay between polls, at least 10
seconds; each poll normally makes one search request, which spends the OAuth
app's Confluence rate-limit quota. The `space-page-changes` binding of
`pageChanged` uses the space picker, which saves the watched space's ID, key,
and name; leave it unsaved to watch every space the account can view. The
Worker reads saved values at startup, so restart it after saving; a changed
space records a new baseline.

## Run

From `connectors/atlassian/confluence/typescript`, generate and lint strict
FDG 2.0:

```sh
npm run build
npm install --prefix examples/page-changed --no-save --no-package-lock --ignore-scripts .
cd examples/page-changed
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/page-changed --open=false
```

Start `dexcli dev -flow-rendering-dir ./build/page-changed`, then run the
Worker and the Trigger with the connections file Dex Web wrote:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json npm run example:changes
```

[`app.ts`](app.ts) wires the binding to the Flow:

```ts
const runner = newProjectPageTriggerRunner(project, ConnectionName, [{
  bindingName: PageChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryPageChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
}], confluenceOptions);
```

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline and changes made while it
was stopped are not delivered. Set `LOG_LEVEL` (`debug`, `info`, `warn`,
`error`) for the Trigger records.

## Test

`test/page_changed_workflow.test.ts` covers the Flow's mapping, validation,
binding declaration, and the project runner's saved and unsaved bindings. With
`dexcli dev` running, the integration tests run a real Dex Worker and Client
against a local Confluence stand-in: the baseline starts no Flow, other spaces
are ignored, each page version starts one Flow across a Trigger restart whose
first search answers 429, and a changed space records a new baseline:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Live Confluence search has not been verified.
