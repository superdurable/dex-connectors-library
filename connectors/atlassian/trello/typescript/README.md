# Trello connector for TypeScript

Install `@superdurable/connector-atlassian-trello` with `@superdurable/connectors-sdk` and `@superdurable/dex`. The package binds a private named connection to five Trello operations, `listCards`, `getCard`, `createCard`, `updateCard`, and `addComment`, and to the `cardChanged` Trigger.

The [approved board example](examples/approved-board-cards/README.md) is a runnable Dex Flow with a Worker and starter. `createCard` and `addComment` checkpoint their single dispatch before sending; a response whose outcome cannot be confirmed returns `uncertain`. `updateCard` sets absolute fields and reads the card back before reporting `updated`.

The package also implements the `cardChanged` poll Trigger at parity with the Go connector, on the SDK's durable poll Trigger kit (`@superdurable/connectors-sdk/pollingtrigger`, 0.12.0, which requires `@superdurable/dex` 1.45.4 or newer). Each binding polls one board's action history every `pollInterval` (at least 10 seconds; blank keeps 30 seconds), re-reads the last minute, skips the action IDs it already delivered, and delivers the rest oldest first, 500 per page. The first poll records a baseline and delivers no earlier action. A backlog over 10,000 actions, a 400 that rejects `since` or `before`, a cursor for another board, or an undecodable cursor records a new baseline; a 429 pauses every binding of the connection for one 10-second rate-limit window. Delivery is at least once, and each action keeps the Trigger event ID `trello-action-<action ID>` across refetches. Cursor tokens and inbox payloads are byte-compatible with the Go connector; `TriggerEvent.occurredAt` is a `Date`, so it keeps milliseconds, while the payload's `occurredAt` keeps Trello's full time.

- `newProjectCardTriggerRunner(project, connectionName, routes, options)` opens the connection, loads each binding's saved board, and records every action in the binding's project Trigger inbox before its poll cursor commits. A binding without a saved board throws.
- `newCardTriggerRunner({ connection, cardChangedRoutes })` takes targets and cursor stores directly; `newCardChangedTrigger(...)` keeps its cursor in memory and records a new baseline on every start.
- `defineCardChangedTriggerBinding(...)` declares a binding with its `boardInput` unit; `TrelloClientOptions.triggerLogger` receives the runner's records.

The [card-changed example](examples/card-changed/README.md) is a runnable Flow started by `cardChanged`.

The [connector guide](../README.md) documents API key and token setup, card field rules, and branch behavior. Keep the key and token in the Dex connection; Flow input contains only card and board IDs and nonsecret content.

Run package verification with `npm run check`. Run provider replay with `npm test`; the conformance suite compares every operation with Go recordings, and Go-generated fixtures check the Trigger's requests, cursor tokens, and events byte for byte. Run the Dex integration test against `dexcli dev` using `npm run test:integration`. Live Trello behavior has not been verified for this TypeScript package.
