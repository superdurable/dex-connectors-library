# Hiver connector for Dex TypeScript

`@superdurable/connector-hiver` implements all six Hiver operations: `listInboxes`, `listConversations`, `getConversation`, `updateConversation`, `addNote`, and `createSharedDraft`. It uses the generated operation-specific Step factories and one static Dex connection. `updateConversation` applies absolute status, assignee, and tag changes and reads them back. Notes and shared drafts use the SDK single-dispatch fence, so an ambiguous write selects `uncertain` without sending it again.

Connection values of zero for `maxResponseBytes` and `requestIntervalMilliseconds` use the connector defaults, matching the Go package.

## Runnable example

The checked-in [list-inboxes example](examples/list-inboxes/README.md) is a minimal consuming Flow. Configure the static `hiver-support` connection in Dex Web with the API key and the optional response and request-spacing settings described in the [connector README](../README.md). Start `dexcli dev`, then run `npm run example:list` from this package. Start the Flow using `npm run example:list:start -- examples/list-inboxes/input.example.json`.

For the credential-free real Dex integration test, run `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` set to the address of a running Dex dev server. It runs the same Flow through a real Worker and Client against a local Hiver-compatible HTTP server.

## Verification

From the repository root, regenerate and check the manifest contracts with:

```sh
GOWORK=off go run ./cmd/connectorctl generate connectors/hiver/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate --check connectors/hiver/connector.yaml
```

Run `npm ci && npm run check` from this package. The TypeScript conformance suite replays the Go recordings in `conformance/connectors/hiver/scenarios.json`, including multipart writes, retry decisions, and uncertain outcomes. Live Hiver authorization and provider behavior remain unverified.
