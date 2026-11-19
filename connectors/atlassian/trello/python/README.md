# Trello Python connector

`dex-connector-atlassian-trello` implements the `listCards`, `getCard`, `createCard`, `updateCard`, and `addComment` Steps from `connector.yaml`. Create and comment calls use a Dex heartbeat fence before sending; ambiguous outcomes select `uncertain` and are never resent automatically. `updateCard` uses absolute values and reads the card back to confirm the update.

See [`examples/approved_request_card`](examples/approved_request_card) for a runnable Dex Flow. Configure its static `ConnectionName` through Dex Web Connections with a Trello API key and user token. Provider authorization and live Trello behavior are not verified by local replay tests.
