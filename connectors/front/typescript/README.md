# Front TypeScript connector

`@superdurable/connector-front` supplies the five Front conversation and contact operations for Dex TypeScript Flows. Operation-specific Step factories, model validators, connection contracts, and mocks are generated from [`connector.yaml`](../connector.yaml) and the Go model types. Run `go run ./cmd/connectorctl generate connectors/front/connector.yaml` from the repository root after changing either source.

Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or newer. Create a secret Front API token in [Front Settings > Developers > API Tokens](https://app.frontapp.com/), grant the read, write, and send permissions needed by your Flow, and save it as the `api_token` credential in a Dex Web Connection. At Worker startup, load the project with `loadFromEnvironment()` and pass the static `ConnectionName` to `newProjectConnection`, as in the [runnable example](examples/find-contact/). Keep the token out of Flow inputs, Attributes, results, Streams, and logs.

| Factory | Front operation | Happy branch |
| --- | --- | --- |
| `newSearchConversationsStep` | Search one bounded conversation page | `searched` |
| `newGetConversationStep` | Read one conversation with messages and comments | `found` |
| `newReplyToConversationStep` | Send a reply or add an internal comment | `replied` |
| `newUpdateConversationStep` | Set status, assignee, and tags | `updated` |
| `newFindContactByEmailStep` | Look up a contact email alias | `found` |

Every other branch is optional, so an unwired branch fails the Flow. Replies and comments use the SDK single-dispatch fence: Front has no provider idempotency key, and an ambiguous send selects `uncertain` rather than sending twice. Updates set absolute values and read back the conversation, so they can retry safely.

Run `npm ci`, `npm run check`, and `npm run test:integration` with a local `dexcli dev` address. The package replays the Go-recorded provider scenarios; the integration test runs the checked-in Flow through a real Dex Worker and Client against a local Front-compatible fixture. Live Front authorization, conversation operations, and contact lookup have not been verified.
