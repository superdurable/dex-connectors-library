# Gmail connector for TypeScript

Install the connector in a Dex application:

```sh
npm install @superdurable/connector-google-gmail @superdurable/dex
```

This package provides typed `getMessage`, `sendMessage`, and `replyToMessage` operations. Its operation identities, branches, retry policy, authorization fields, and mock cases mirror [the manifest](https://github.com/superdurable/dex-connectors-library/blob/main/connectors/google/gmail/connector.yaml). From 0.3.0 it also provides the `messageReceived` and `replyReceived` Triggers and their shared, ordered runner, at parity with the Go connector.

The checked-in `src/generated/` contracts come from `connector.yaml` and the
Go operation structs. Run `go run ./cmd/connectorctl generate --lang typescript connectors/google/gmail/connector.yaml` from the repository root after either source changes. `src/models.ts` keeps credential requirements specific to Google OAuth and Workspace delegation, plus the public non-null send recipient arrays. `src/steps.ts` delegates to generated factories while requiring `GmailConnection` at compile time; `src/mock/index.ts` re-exports matching generated scenarios and validates send scenarios against the public result type. `src/project.ts` keeps the refresh codec and public connection constructor.

Version 0.3.0 requires Connector SDK 0.11.0. Generated private and shared
credential decoders validate project material, and the refresh driver reads
generated manifest grants with Gmail-specific delegation checks. Each token
exchange honors the declared `tokenExchangeTimeout`, as the Go driver does.

Send and reply recipients are validated as mailboxes and returned in canonical header form, such as `<ada@example.com>` or `"Ada" <ada@example.com>`.
Decoded messages omit absent optional metadata and body fields, matching the Go connector's JSON output.

## Configure a connection

In Dex Web **Connections**, create a Google Gmail connection with the static name used by the application's `ConnectionName` constant. The application loads project settings through the Connector SDK and resolves credentials during each Step attempt. Refreshes update encrypted project storage or Dex Web's private local `connections.json`; tokens and service-account keys must never be put in Flow input or persisted state.

For personal Gmail or ordinary Workspace accounts, select **Google OAuth**. In [Google Cloud Credentials](https://console.cloud.google.com/apis/credentials), create a Web application, enable the Gmail API, and add the exact redirect URI shown by Dex Web. Configure the consent audience, test users when the app is in Testing, and the scopes shown by Dex Web. Paste the client ID and client secret into the connection form; Dex Web derives the verified primary email and stores the access and refresh tokens after consent.

For administrator-managed Workspace delegation, use [Google Cloud service accounts](https://console.cloud.google.com/iam-admin/serviceaccounts) to create a service account and download its JSON key. In [Workspace domain-wide delegation](https://admin.google.com/ac/owl/domainwidedelegation), authorize that account's OAuth client ID for the Gmail readonly and send scopes. Put the JSON key and one managed mailbox in the connection form. The connector mints short-lived delegated tokens and uses that mailbox as the sender.

`sendMessage` and `replyToMessage` use synchronous Execute and claim a Dex heartbeat checkpoint before POST. They search Sent Mail under a run-specific RFC `Message-ID` after an ambiguous send. A confirmed match returns `sent`. Both operations declare `repeatOnUnknown: limited` with `maxSends: 3`, and the Fence takes the definition's cap or the Step's `maxSends`. After a claimed send whose outcome stayed unknown, a Sent Mail miss or failed search resends with the same `Message-ID` while fewer than that many sends may have taken effect; at the cap a miss retries read-back without another POST until the seven-attempt, five-minute budget fails the Flow. A saved Gmail message ID and an allowlisted 4xx refusal are never resent. An allowlisted 4xx refusal, or a rejected or unusable reply source read, selects its branch only when no earlier send is possible; otherwise it returns Retry, because the earlier send may still be delivered. A POST status of 400, 401, 403, 404, 411, 413, 414, 415, or 422 with no earlier possible send proves Gmail refused it before processing, so it records `canResend` and selects `providerRejected`. Any other 4xx except 429, such as 408 or 409, leaves the outcome unknown: it returns Retry and keeps the claim, so the next attempt reads back Sent Mail and resends below the cap. A conclusive 429 or a failure proven to precede dispatch can permit another POST. A 503 may have been processed, so like any 5xx it keeps the claim. An accepted 2xx never resends merely because its response body is unusable.

## Triggers

`messageReceived` receives a matching message that begins a Gmail thread and `replyReceived` a matching reply in an existing thread. Both poll the newest inbox page (`pollPageSize` messages, `labelIds=INBOX`, plus the binding's optional Gmail `searchQuery`) every `pollInterval`, read each new message's metadata, and emit a metadata-only `MessageEvent` (`primaryEmail`, `messageId`, `threadId`, `from`, `subject`, `snippet`, `receivedAt`, `isReply`) whose event ID is the Gmail message ID. The binding's `messageMatcher` or `replyMatcher` requires a case-insensitive `messageContains` substring of the subject or snippet and an optional `senderEmails` allowlist.

- `newProjectMessageTriggerRunner(project, connectionName, { messageReceivedRoutes, replyReceivedRoutes }, options)` loads each binding's saved configuration and wraps its target in the binding's durable project inbox, so a restarted or replaced replica replays acknowledged events first. Wire targets with `newDexFlowTriggerTarget` and `newDexRPCTriggerTarget` from `@superdurable/connectors-sdk`. `newMessageTriggerRunner` takes configurations directly.
- Each poll lists every reply route before any root route, then delivers every root before any reply, so a root starts its Flow before its replies reach it. Delivery stops at the first failure; every later poll retries that event first, even after newer mail pushes it off the page. An undeliverable event is consumed.
- The delivered set covers the last listed page in memory, as in Go. A restart rescans the current page; the Flow start request ID and the RPC target's stable request ID absorb the duplicates.
- Records go to the client options' `logger` (default `defaultTriggerLogger`): WARN `gmail poll failed; retrying` with `attempt` and `delay_ms`, WARN `trigger delivery failed; retrying`, INFO `trigger delivered after retry`, WARN `trigger event skipped: undeliverable`, and DEBUG `trigger event ignored` with `reason` `not_a_reply`, `not_a_root`, or `matcher_mismatch`. Records carry message and thread IDs and error types, never senders, subjects, snippets, search queries, bodies, or tokens.
- `defineMessageReceivedTriggerBinding` and `defineReplyReceivedTriggerBinding` declare static bindings and their configuration units (`searchQueryInput`, `textInput`, `emailListInput`) for Dex Web. `newMessageReceivedTrigger` and `newReplyReceivedTrigger` poll one binding on their own and do not order roots before replies.

The [thread reply example](examples/thread-reply/README.md) starts a Flow from a root message, receives its reply through a typed RPC, and replies in the thread. The pollers and the example are tested against a local stand-in for the Gmail API and a real Dex server. Live Gmail polling has not been verified for this package.

## Run the example

[The Gmail Flow examples](https://github.com/superdurable/dex-connectors-library/tree/main/connectors/google/gmail/typescript/examples/gmail-flow) contain one Flow per operation, one Worker, a read starter, and a real Dex integration test. Their static connection name is `gmail-inbox`. The Worker setup below is from the [runnable entrypoint](https://github.com/superdurable/dex-connectors-library/blob/main/connectors/google/gmail/typescript/examples/gmail-flow/main.ts):

```ts
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const registry = new Registry([new GmailReadFlow(connection), new GmailSendFlow(connection), new GmailReplyFlow(connection)]);
```

Run `npm run example:worker` after configuring the named connection, then `npm run example:start -- GMAIL_MESSAGE_ID` to start a read. `npm run test:integration` exercises read, send, reply, retry, optional branch failure, and the send checkpoint with a fake provider on a real Dex server. No live Google authorization or delivery has been tested.

Dex CLI v1.28.0 rendered all three example Flows as valid FDG 2.0 graphs with no diagnostics. The published package includes the manifest-derived `dex-connector.json` marker, which supplies connector identity, named branches, and Dex Web connection configuration metadata.

Flows that route from a local Step into a connector Step need Dex CLI v1.28.1 for strict FDG visualization ([issue #362](https://github.com/superdurable/dex-connectors-library/issues/362)). The three examples here start at connector Steps and pass with v1.28.0.
