# Trello connector for TypeScript

Install `@superdurable/connector-atlassian-trello` with `@superdurable/connectors-sdk` and `@superdurable/dex`. The package binds a private named connection to five Trello operations: `listCards`, `getCard`, `createCard`, `updateCard`, and `addComment`.

The [approved board example](examples/approved-board-cards/README.md) is a runnable Dex Flow with a Worker and starter. `createCard` and `addComment` checkpoint their single dispatch before sending; a response whose outcome cannot be confirmed returns `uncertain`. `updateCard` sets absolute fields and reads the card back before reporting `updated`.

The [connector guide](../README.md) documents API key and token setup, card field rules, and branch behavior. Keep the key and token in the Dex connection; Flow input contains only card and board IDs and nonsecret content.

Run package verification with `npm run check`. Run provider replay with `npm test`; the conformance suite compares every operation with Go recordings. Run the Dex integration test against `dexcli dev` using `npm run test:integration`. Live Trello behavior has not been verified for this TypeScript package.
