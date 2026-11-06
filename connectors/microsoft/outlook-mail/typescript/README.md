# Microsoft Outlook Mail connector for TypeScript

Install `@superdurable/connector-microsoft-outlook-mail` with `@superdurable/dex`. This package implements `searchMessages`, `getMessage`, `sendMessage`, `replyToMessage`, `moveMessage`, and `setMessageFlags` from [the Outlook Mail manifest](../connector.yaml). It has no TypeScript Triggers. The TypeScript package starts at version 0.1.0 and pins Connector SDK 0.7.0.

The generated files in `src/generated/` define the manifest contracts, authorization declarations, Step factories, and mock scenarios. The handwritten client runs Graph requests and uses those declarations through `ManifestCredentialRefreshDriver`; the project connection uses the generated credential decoders. Regenerate after manifest or Go model changes with `go run ./cmd/connectorctl generate connectors/microsoft/outlook-mail/connector.yaml` from the repository root.

## Connection

Create a Microsoft Outlook Mail connection in Dex Web named `outlook-mail`, matching [the example Flow](examples/read-message/flow.ts). The delegated method needs an Entra app registration with the Dex Web redirect URI and delegated `Mail.ReadWrite`, `Mail.Send`, and `offline_access` permissions. The app-only method needs one configured mailbox and an application restricted to that mailbox through Exchange Online RBAC for Applications. Follow the [connector setup guide](../README.md#connection-setup) for the field paths and authorization instructions. `newProjectConnection(project, ConnectionName)` loads the named connection at Worker startup; credentials stay in the host store and are resolved for every Step attempt.

`sendMessage` and `replyToMessage` create a marked draft and record Dex heartbeats before dispatch. A retry reuses the draft. Once a send request may have left the Worker, the connector checks the draft's immutable ID for its Sent Items copy and never dispatches it again without a conclusive refusal. A still unconfirmed send selects `uncertain` after bounded checks. The two operations use synchronous Execute durability; the reads, move, and absolute flag update use asynchronous durability.

## Run and verify

[The checked-in example](examples/read-message/flow.ts) reads a message through a named Outlook Mail connection. Start a Dex stack with `dexcli dev`, configure the connection in Dex Web, then run `npm run example:worker` and `npm run example:start -- MESSAGE_ID`. `npm run test:integration` starts `dexcli dev` on ports 40950 and 40951, runs the same Flow through a real Dex Worker and a scripted provider, and checks the completed and unwired optional branch paths. `npm run check` runs TypeScript checks, Go-recorded provider conformance replay, and the build. The conformance fixture covers all six operations, branch outcomes, 429 and 5xx retry, and transport failures at each request boundary; `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/microsoft/outlook-mail` verifies the Go and TypeScript recordings together.

No Microsoft 365 tenant authorization or delivery was performed for this TypeScript port. Real provider authorization, delivery, and app-only mailbox permissions remain unverified.
