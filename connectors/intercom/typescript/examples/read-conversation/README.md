# Read an Intercom conversation

`ReadConversationFlow` starts with `{ "conversationId": "2002",
"latestPartLimit": 10 }`, calls the `getConversation` Query, and completes
with its conversation and latest parts. Its static `ConnectionName` is
`intercom`. In Dex Web Connections, configure that name with the Intercom
`region` and a private `access_token` from the Intercom Developer Hub. See
the connector [setup guide](../../../README.md#intercom-setup).

Run `npm ci` from `connectors/intercom/typescript`, start `dexcli dev`, then
run `npm run example:read-conversation` to start the Worker. The application
loads the project connection from `DEX_PROJECT_*` and accepts
`DEX_FLOW_SERVICE_ADDRESS` for the Dex server. Start
`IntercomReadConversation` with the JSON input above.

For a credential-free Dex integration check, run
`DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run integration:read-conversation`
against `dexcli dev`. It runs this Flow with a scripted Intercom operation
through a real Dex Worker, Client, retry, and failed Flow transition.

The `ReadIntercomConversation` Step declares group `intercom` and its
explanation in [flow.ts](flow.ts). Strict FDG 2.0 generation can be checked
with `dexcli visualize examples/read-conversation/flow.ts --language
typescript --schema-version 2.0 --lint app --json --out
/tmp/intercom-read-conversation` from the package directory.

Dex CLI 1.33.0 currently reports `dynamic_class` for the generated factory's
Step target passed in its configuration object and `dynamic_step_target` for
the registered `QueryStep`. The CLI writes partial JSON but cannot certify
strict FDG 2.0 for this factory shape. The same Flow passes the real Dex
Worker and Client integration check; strict visualization remains unverified.
